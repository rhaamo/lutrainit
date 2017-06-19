package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"github.com/rhaamo/lutrainit/shared/ipc"
	"time"
)

// ServiceName defines the service name
type ServiceName string

// ServiceType defines the service type
type ServiceType string

// Command defines a command string used by Startup or Shutdown
type Command string

func (c Command) String() string {
	return string(c)
}

func parseLine(line string, s *Service) error {
	if strings.HasPrefix(line, "Needs:") {
		specified := strings.Split(strings.TrimPrefix(line, "Needs:"), ",")
		for _, nd := range specified {
			s.Needs = append(s.Needs, ServiceType(strings.TrimSpace(nd)))
		}
	} else if strings.HasPrefix(line, "Provides:") {
		specified := strings.Split(strings.TrimPrefix(line, "Provides:"), ",")
		for _, nd := range specified {
			s.Provides = append(s.Provides, ServiceType(strings.TrimSpace(nd)))
		}
	} else if strings.HasPrefix(line, "Startup:") {
		if s.Startup != "" {
			return fmt.Errorf("Startup already set")
		}
		s.Startup = Command(strings.TrimSpace(strings.TrimPrefix(line, "Startup:")))
	} else if strings.HasPrefix(line, "Shutdown:") {
		if s.Shutdown != "" {
			return fmt.Errorf("Shutdown already set")
		}
		s.Shutdown = Command(strings.TrimSpace(strings.TrimPrefix(line, "Shutdown:")))
	} else if strings.HasPrefix(line, "Name:") {
		if s.Name == "" {
			s.Name = ServiceName(strings.TrimSpace(strings.TrimPrefix(line, "Name:")))
		}
	} else if strings.HasPrefix(line, "Description:") {
		if s.Description == "" {
			s.Description = strings.TrimSpace(strings.TrimPrefix(line, "Description:"))
		}
	} else if strings.HasPrefix(line, "PIDFile:") {
		if s.PIDFile == "" {
			s.PIDFile = strings.TrimSpace(strings.TrimPrefix(line, "PIDFile:"))
		}
	} else if strings.HasPrefix(line, "Type:") {
		if s.Type == "" {
			serviceType := strings.TrimSpace(strings.TrimPrefix(line, "Type:"))
			switch serviceType {
			case "simple":
				s.Type = "simple"
			case "forking":
				s.Type = "forking"
			case "oneshot":
				s.Type = "oneshot"
			default:
				fmt.Printf("Invalid service type: %s, forcing Type=simple\n", serviceType)
				s.Type = "simple"
			}
		}
	}
	return nil
}

func parseSetupLine(line string) (autologin string, persist bool) {
	if strings.HasPrefix(line, "Autologin:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "Autologin:")), false
	} else if strings.HasPrefix(line, "Persist:") {
		return "", strings.TrimSpace(strings.TrimPrefix(line, "Persist:")) == "true"
	}
	return "", false
}

// ParseConfig a single config file into the services it provides
func ParseConfig(r io.Reader, filename string) (Service, error) {
	s := Service{}
	var line string
	var err error
	scanner := bufio.NewReader(r)

	for {
		line, err = scanner.ReadString('\n')
		switch err {
		case io.EOF:
			if err := parseLine(line, &s); err != nil {
				log.Println(err)
			}

			// Check for configuration sanity before returning
			if err := checkSanity(&s, filename); err != nil {
				return Service{}, err
			}

			return s, nil
		case nil:
			if err := parseLine(line, &s); err != nil {
				log.Println(err)
			}
		default:
			return Service{}, err
		}
	}
}

// ParseServiceConfigs parse all the config in directory dir return a map of
// providers of ServiceTypes from that directory.
func ParseServiceConfigs(dir string, reloading bool) error {
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, fstat := range files {
		if fstat.IsDir() {
			// Mostly to skip "." and ".."
			continue
		}

		// We only want to parse files ending with .service
		if !strings.HasSuffix(fstat.Name(), ".service") {
			continue
		}

		f, err := os.Open(dir + "/" + fstat.Name())
		if err != nil {
			log.Println(err)
			continue
		}
		s, err := ParseConfig(f, fstat.Name())
		f.Close()
		if err != nil {
			log.Println(err)
			continue
		}

		for _, t := range s.Provides {
			StartupServices[t] = append(StartupServices[t], &s)
		}

		// Populate the Loaded Services thingy
		ipcLoadedService :=  &ipc.LoadedService{
			Name: ipc.ServiceName(s.Name),
			Description: s.Description,
			PIDFile: s.PIDFile,
			Type: s.Type,
		}

		// If we are not reloading, set initial state and actions
		if !reloading {
			ipcLoadedService.State = ipc.NotStarted
			ipcLoadedService.LastAction = ipc.Unknown
			ipcLoadedService.LastActionAt = time.Now().UTC().Unix()
		}

		LoadedServices[ipc.ServiceName(s.Name)] = ipcLoadedService

	}
	return nil
}

func checkSanity(service *Service, filename string) error {

	if !ipc.IsCustASCII(string(service.Name)) {
		return fmt.Errorf("%s has invalid service name '%s', only a-Z0-9_-. allowed", filename, service.Name)
	}

	for _, provide := range service.Provides {
		if !ipc.IsCustASCII(string(provide)) {
			return fmt.Errorf("%s has invalid provides '%s', only a-Z0-9_-. allowed", filename, provide)
		}
	}

	for _, need := range service.Needs {
		if !ipc.IsCustASCII(string(need)) {
			return fmt.Errorf("%s has invalid needs '%s', only a-Z0-9_-. allowed", filename, need)
		}
	}

	return nil
}

// ParseSetupConfig parse the main configuration
func ParseSetupConfig(r io.Reader) (autologins []string, persist bool, err error) {
	scanner := bufio.NewReader(r)
	for {
		line, err2 := scanner.ReadString('\n')
		switch err2 {
		case io.EOF:
			autologin, persist2 := parseSetupLine(line)
			if autologin != "" {
				autologins = append(autologins, autologin)
			}
			if persist2 {
				persist = persist2
			}
			return
		case nil:
			autologin, persist2 := parseSetupLine(line)
			if autologin != "" {
				autologins = append(autologins, autologin)
			}
			if persist2 {
				persist = persist2
			}
		default:
			err = err2
			return
		}
	}
}
