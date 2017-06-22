package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
	"github.com/go-clog/clog"
	"io/ioutil"
	"strconv"
	"bytes"
	"github.com/mitchellh/go-ps"
	"syscall"
)

const (
	errWaitNoChild   = "wait: no child processes"
	errWaitIDNoChild = "waitid: no child processes"
)

// ServiceName defines the service name
type ServiceName string

// ServiceType provides or needs
type ServiceType string

type RunState uint8

// Types of valid runState
const (
	NotStarted = RunState(iota)
	Starting
	Started
	Stopped
	Errored
)

func (rs RunState) String() string {
	switch rs {
	case NotStarted:
		return "not started"
	case Starting:
		return "being started"
	case Started:
		return "already started"
	case Stopped:
		return "stopped"
	case Errored:
		return "errored"
	default:
		return "in an invalid state"
	}
}

// LastAction represent the latest action done to the service
type LastAction uint8

// Last actions constants
const (
	Unknown = LastAction(iota)
	Start
	Stop
	Reload
	Restart
	Forcekill
)

func (la LastAction) String() string {
	switch la {
	case Unknown:
		return "unknown"
	case Start:
		return "start"
	case Stop:
		return "stop"
	case Reload:
		return "reload"
	case Restart:
		return "restart"
	case Forcekill:
		return "force kill"
	default:
		return "really unknown"
	}
}

// Command defines a command string used by Startup or Shutdown
type Command string

func (c Command) String() string {
	return string(c)
}

type StartupService struct {
	Name		ServiceName
	AutoStart	bool

	Provides 	[]ServiceType
	Needs    	[]ServiceType
}

// Service represents a struct with usefull infos used for management of services
type Service struct {
	Name		ServiceName
	AutoStart	bool

	Provides 	[]ServiceType
	Needs    	[]ServiceType

	Description		string		// Currently not used
	State			RunState

	LastAction		LastAction
	LastActionAt	int64		// Timestamp of the last action (UTC)
	LastMessage		string
	LastKnownPID	int

	Type			string // forking or simple
	PIDFile			string

	Startup  	Command
	Shutdown 	Command
	CheckAlive  Command

	Deleted			bool
}

// StartServices starts all declared services at start
func StartServices() {
	wg := sync.WaitGroup{}

	var startedMu = &sync.RWMutex{}
	startedTypes := make(map[ServiceType]bool)
	for _, services := range StartupServices {
		wg.Add(len(services))
		for _, s := range services {
			lS := LoadedServices[s.Name]
			go func(s *Service) {
				// TODO: This should ensure that Needs are satisfiable instead of getting into an
				// infinite loop when they're not.
				// (TODO(2): Prove N=NP in order to do the above efficiently.)
				for satisfied, tries := false, 0; satisfied == false && tries < 60; tries++ {
					satisfied = s.NeedsSatisfied(startedTypes, startedMu)
					time.Sleep(2 * time.Second)

				}
				if s.State == NotStarted && s.AutoStart {
					// Start the service
					if s.Type == "oneshot" || s.Type == "forking" {
						if err := s.Start(); err != nil {
							clog.Error(2, err.Error())
						}
					} else if s.Type == "simple" {
						go s.StartSimple()
					} else {
						// What are you doing here ?
						clog.Warn("I don't know why but I'm asked to start %s with type %s\n", s.Name, s.Type)
					}

				}

				startedMu.Lock()
				for _, t := range s.Provides {
					startedTypes[t] = true
				}
				startedMu.Unlock()
				wg.Done()
			}(lS)
		}
	}
	wg.Wait()
}

// Start the Service s. if type is oneshot or forking
func(s Service) Start() error {
	if s.State != NotStarted {
		return fmt.Errorf("Service %v is %v", s.Name, s.State.String())
	}
	s.State = Starting
	LoadedServices[s.Name].State = Starting
	LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
	LoadedServices[s.Name].LastAction = Start

	cmd := exec.Command("sh", "-c", s.Startup.String())
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Run(); err != nil {
		if err.Error() == errWaitNoChild || err.Error() == errWaitIDNoChild {
			// Process exited cleanly
			s.State = Started
			LoadedServices[s.Name].State = Started
			LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
			LoadedServices[s.Name].LastAction = Start

			clog.Info("[lutra] Started service %s", s.Name)

			return nil
		}

		s.State = Errored
		LoadedServices[s.Name].State = Errored
		LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
		LoadedServices[s.Name].LastAction = Start

		clog.Error(2,"[lutra] Error starting service %s: %s", s.Name, err.Error())

		return err
	}
	s.State = Started
	LoadedServices[s.Name].State = Started
	LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
	LoadedServices[s.Name].LastAction = Start

	clog.Info("[lutra] Started service %s", s.Name)

	return nil
}

// StartSimple and track the PID and process state (for simple service without auto-fork function)
func(s Service) StartSimple() {
	s.State = Starting
	LoadedServices[s.Name].State = Starting
	LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
	LoadedServices[s.Name].LastAction = Start
	LoadedServices[s.Name].LastKnownPID = 0

	cmd := exec.Command("sh", "-c", s.Startup.String())
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		clog.Error(2,"[lutra] Service %s exited with error: %s", s.Name, err.Error())
		s.State = Errored
		LoadedServices[s.Name].State = Errored
		LoadedServices[s.Name].LastMessage = err.Error()
		LoadedServices[s.Name].LastKnownPID = 0
		return
	}
	// Waiting for the command to finish
	s.State = Started
	LoadedServices[s.Name].State = Started
	LoadedServices[s.Name].LastKnownPID = cmd.Process.Pid
	clog.Info("[lutra] Started service %s", s.Name)

	err := cmd.Wait()
	if err != nil {
		clog.Error(2, "[lutra] Service %s finished with error: %s", s.Name, err.Error())
		s.State = Stopped
		LoadedServices[s.Name].State = Stopped
		LoadedServices[s.Name].LastMessage = err.Error()
		LoadedServices[s.Name].LastKnownPID = 0
	} else {
		s.State = Stopped
		clog.Info("[lutra] Service stopped:	 %s", s.Name)
		LoadedServices[s.Name].State = Stopped
		LoadedServices[s.Name].LastKnownPID = 0
		LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
		LoadedServices[s.Name].LastAction = Stop
	}
}

// NeedsSatisfied if all of s's needs are satified by the passed list of provided types
func (s Service) NeedsSatisfied(started map[ServiceType]bool, mu *sync.RWMutex) bool {
	mu.RLock()
	defer mu.RUnlock()
	for _, st := range s.Needs {
		if !started[st] {
			return false
		}
	}
	return true
}

func getProcessPid(s *Service) (pid int, err error) {
	d, err := ioutil.ReadFile(s.PIDFile)
	if err != nil {
		return 0, err
	}

	pid, err = strconv.Atoi(string(bytes.TrimSpace(d)))
	if err != nil {
		return 0, fmt.Errorf("error parsing pid from: %s", s.PIDFile)
	}

	return pid, nil
}

func processAliveByPid(pid int) (alive bool, err error) {
	if pid == 0 {
		return false, fmt.Errorf("why are you asking me if PID 0 is alive ?")
	}

	_, err = ps.FindProcess(pid)
	if err != nil {
		return false, err
	}

	return true, nil
}

// returns true if command successfull, else always false
func processAliveByCmd(command string) (alive bool, err error) {
	cmd := exec.Command("sh", "-c", command)

	if err = cmd.Run(); err != nil {
		return false, err
	}
	// did the command fail because of an unsuccessful exit code
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}

	return true, nil
}

// ExecShutdown of the specified service
func ExecShutdown(command string) (err error) {
	cmd := exec.Command("sh", "-c", command)

	if err = cmd.Run(); err != nil {
		return err
	}
	// did the command fail because of an unsuccessful exit code
	if _, ok := err.(*exec.ExitError); ok {
		return nil
	}

	return nil
}

func checkIfProcessAlive(s *Service) (alive bool, pid int, err error) {
	// Check using PID
	if s.PIDFile != "" {
		if _, err := os.Stat(s.PIDFile); os.IsNotExist(err) {
			return false, 0, nil // NO PID
		}

		pid, err := getProcessPid(s)
		if err != nil {
			return false, 0, err
		}
		running, err := processAliveByPid(pid)
		if err != nil {
			return false, 0, err
		}
		return running, pid, nil
	}

	// Check using CheckAlive
	if s.CheckAlive != "" {
		running, err := processAliveByCmd(s.CheckAlive.String())
		if err != nil {
			return false, 0, err
		}
		return running, 0, nil
	}

	// TODO: some sort of 'pgrep blah' fork forking types

	// Else if it's a simple, check status from list
	if s.Type == "simple" {
		return s.State == Started, 0, nil
	}

	// Cannot determine process state
	return true, 0, fmt.Errorf("cannot determine process state")
}

func CheckAndStartService(s *Service) (err error) {
	alive, pid, err := checkIfProcessAlive(s)
	if err != nil {
		return err
	}

	if alive && pid != 0 {
		return fmt.Errorf("process %s already running as PID %d", s.Name, pid)
	} else if alive {
		return fmt.Errorf("process %s already running", s.Name)
	}

	// start service
	if s.Type == "simple" {
		go s.StartSimple()
	} else {
		s.Start()
	}

	return nil
}

func CheckAndStopService(s *Service) (err error) {
	// Well, we don't really care if process is running, yeah ?
	LoadedServices[s.Name].LastActionAt = time.Now().UTC().Unix()
	LoadedServices[s.Name].LastAction = Stop

	// If simple check struct status
	if s.Type == "simple" {
		if s.State == Starting || s.State == Started {
			// kill process according to cmd Shutdown
			if s.Shutdown != "" {
				err = ExecShutdown(s.Shutdown.String())
			} else {
				err = ExecShutdown(fmt.Sprintf("pkill %d", s.LastKnownPID))
			}
			if err != nil {
				LoadedServices[s.Name].State = Errored
				return err
			}
			LoadedServices[s.Name].State = Stopped
			return err
		}
		LoadedServices[s.Name].State = Stopped
		return fmt.Errorf("process %s doesn't seems to be alive ?", s.Name)
	} else {
		if s.Shutdown != "" {
			err = ExecShutdown(s.Shutdown.String())
		} else {
			err = fmt.Errorf("no Shutdown: command defined for %s, I don't know how to kill it", s.Name)
		}
		if err != nil {
			LoadedServices[s.Name].State = Errored
			return err
		}
		LoadedServices[s.Name].State = Stopped
		return err
	}
}