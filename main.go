package main

import (
	"encoding/json"
	"flag"
	"github.com/jgulick48/openevse-statsd/internal/metrics"
	"github.com/jgulick48/openevse-statsd/internal/models"
	"github.com/jgulick48/openevse-statsd/internal/openevse"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/mitchellh/panicwrap"
)

var configLocation = flag.String("configFile", "./config.json", "Location for the configuration file.")

func main() {
	startService()
	exitStatus, err := panicwrap.BasicWrap(panicHandler)
	if err != nil {
		// Something went wrong setting up the panic wrapper. Unlikely,
		// but possible.
		panic(err)
	}

	// If exitStatus >= 0, then we're the parent process and the panicwrap
	// re-executed ourselves and completed. Just exit with the proper status.
	if exitStatus >= 0 {
		os.Exit(exitStatus)
	}
}

func panicHandler(output string) {
	// output contains the full output (including stack traces) of the
	// panic. Put it in a file or something.
	log.Printf("The child panicked:\n\n%s\n", output)
	os.Exit(1)
}

func startService() {
	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}
	log.Print(path)
	config := LoadClientConfig(*configLocation)
	if config.StatsServer != "" {
		metrics.Metrics, err = statsd.New(config.StatsServer)
		if err != nil {
			log.Printf("Error creating stats client %s", err.Error())
		} else {
			metrics.StatsEnabled = true
			log.Printf("Got stats server %s stats are enabled\n", config.StatsServer)
		}
	}
	openEVSEClient := openevse.NewClient(config.EVSEConfiguration, http.DefaultClient)
	done := make(chan bool)
	OnTermination(func() {
		openEVSEClient.Stop()
		done <- true
	})
	for <-done {
		return
	}
}

// TermFunc defines the function which is executed on termination.
type TermFunc func()

func OnTermination(fn TermFunc) {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	signal.Notify(c, os.Kill)
	signal.Notify(c, syscall.SIGTERM)

	go func() {
		select {
		case <-c:
			if fn != nil {
				fn()
			}
		}
	}()
}

func LoadClientConfig(filename string) models.Config {
	if filename == "" {
		filename = "./config.json"
	}
	configFile, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Printf("No config file found. Making new IDs")
		panic(err)
	}
	var config models.Config
	err = json.Unmarshal(configFile, &config)
	if err != nil {
		log.Printf("Invliad config file provided")
		panic(err)
	}
	return config
}
