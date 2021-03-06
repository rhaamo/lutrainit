package main

import (
	"dev.sigpipe.me/dashie/lutrainit/shared/ipc"
	"github.com/urfave/cli"
	"github.com/valyala/gorpc"
	"os"
	"os/user"
)

var (
	// LutraVersion should match the one in lutrainit/main.go
	LutraVersion = "0.1"

	// GorpcDispatcher is the main dispatcher object
	GorpcDispatcher *gorpc.Dispatcher
	// GorpcDispatcherClient is the client dispatcher object
	GorpcDispatcherClient *gorpc.DispatcherClient
	// GorpcClient is the main client object
	GorpcClient *gorpc.Client

	// Theses two last should only filled by LDFLAGS, see Makefile

	// LutraBuildTime is the time of the build
	LutraBuildTime string
	// LutraBuildGitHash is the git sha1 of the commit based on
	LutraBuildGitHash string
)

func main() {
	app := cli.NewApp()
	app.Name = "lutractl"
	app.Usage = "lutra init control client"
	app.Version = LutraVersion
	app.Commands = []cli.Command{
		CmdVersion,
		CmdStats,
		CmdStatus,
		CmdReboot,
		CmdShutdown,
		CmdReload,
		CmdStart,
		CmdStop,
		CmdRestart,
		CmdReexec,
	}
	app.Flags = append(app.Flags, []cli.Flag{}...)

	// Main RPC initialization
	gorpc.RegisterType(&ipc.SysStatus{})
	gorpc.RegisterType(&ipc.Version{})
	gorpc.RegisterType(&ipc.AskStatus{})
	gorpc.RegisterType(&ipc.AnswerReload{})
	gorpc.RegisterType(&ipc.ServiceAction{})
	gorpc.RegisterType(&ipc.ServiceActionAnswer{})

	GorpcDispatcher = gorpc.NewDispatcher()

	GorpcDispatcher.AddFunc("status", func(status *ipc.AskStatus) map[ipc.ServiceName]*ipc.Service {
		println("wanting client status")
		return nil
	})

	GorpcClient = gorpc.NewUnixClient("/run/ottersock")
	GorpcClient.Start()
	defer GorpcClient.Stop()

	GorpcDispatcherClient = GorpcDispatcher.NewFuncClient(GorpcClient)

	// Let's go baby
	app.Run(os.Args)
}

// IsRoot will tell you if you are root
func IsRoot() bool {
	curUser, err := user.Current()
	if err != nil {
		return false
	}

	// Yeah for whatever reason Uid and Gid are strings
	return curUser.Uid == "0" && curUser.Gid == "0"
}
