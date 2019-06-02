package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/signal"

	"github.com/sirupsen/logrus"
	"github.com/emicklei/go-restful"
)

// These are the error codes returned.
const (
	ExitLoadConfigError = iota
	ExitParseConfigError
)

type (
	// Spriteful handles the API endpoints.
	Spriteful struct {
		BindHost   string   `json:"bind-host"`
		BindPort   int      `json:"bind-port"`
		Servers    []Server `json:"servers"`
	}

	// Server represents a server with it's boot configuration.
	Server struct {
		MacAddress  string   `json:"mac"`
		Kernel      string   `json:"kernel"`
		Initrd      []string `json:"initrd"`
		CommandLine string   `json:"cmdline"`
	}

	// PixieResponse is the response required by pixie core for booting up servers.
	PixieResponse struct {
		Kernel      string   `json:"kernel"`
		Initrd      []string `json:"initrd"`
		CommandLine string   `json:"cmdline"`
	}
)

// Starts Spriteful API using the provided configuration.
func main() {
	logrus.Info("Starting Spriteful API...")
	config := flag.String("config", "config.json", "spriteful configuration")
	flag.Parse()
	data, err := ioutil.ReadFile(*config)
	if err != nil {
		logrus.WithField(logrus.ErrorKey, err).Fatal("unable to read config")
		os.Exit(ExitLoadConfigError)
	}
	var sprite Spriteful
	if err := json.Unmarshal(data, &sprite); err != nil {
		logrus.WithField(logrus.ErrorKey, err).Fatal("unable to parse config.")
		os.Exit(ExitParseConfigError)
	}
	logrus.Infof(`Config "%s" loaded.`, *config)
	sprite.startApi()
}

// Starts the Spriteful API.
func (s *Spriteful) startApi() {
	container := restful.NewContainer()
	s.register(container)

	bindAddress := net.JoinHostPort(s.BindHost, strconv.Itoa(s.BindPort))
	server := &http.Server{
		Addr:    bindAddress,
		Handler: container,
	}
	go server.ListenAndServe()
	logrus.Infof(`Spriteful API now listening at "%s".`, bindAddress)

	ch := make(chan os.Signal)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-ch
	logrus.Info("Shutting down Spriteful API...")
}

// Registers the endpoints for the API.
func (s *Spriteful) register(container *restful.Container) {
	logrus.Info("Creating API endpoints...")

	ws := &restful.WebService{}
	ws.Path("/api/v1")

	ws.Route(ws.GET("boot/{mac-addr}").To(s.handleBootRequest).
		Consumes(restful.MIME_JSON).
		Produces(restful.MIME_JSON).
		Param(ws.PathParameter("mac-addr", "the mac address")).
		Writes(PixieResponse{}))
	logrus.Info(`pixiecore endpoint created at "api/v1/boot/{mac}".`)

	container.Add(ws)
}

// Handles the http request for server boot configuration.
func (s *Spriteful) handleBootRequest(req *restful.Request, res *restful.Response) {
	logrus.Info("Received pixiecore request...")
	macAddress := req.PathParameter("mac-addr")
	server, err := s.findServerConfig(macAddress)
	if err != nil {
		res.WriteError(http.StatusNotFound, err)
		return
	}

	str, err := json.Marshal(&PixieResponse{
		Kernel:      server.Kernel,
		Initrd:      server.Initrd,
		CommandLine: server.CommandLine,
	})
	if err != nil {
		res.WriteError(http.StatusBadRequest, err)
		return
	}

	str = bytes.Replace(str, []byte("\\u003c"), []byte("<"), -1)
	str = bytes.Replace(str, []byte("\\u003e"), []byte(">"), -1)
	str = bytes.Replace(str, []byte("\\u0026"), []byte("&"), -1)

	value := string(str)
	value, err = url.QueryUnescape(value)
	if err != nil {
		res.WriteError(http.StatusBadRequest, err)
		return
	}

	fmt.Fprint(res.ResponseWriter, value)
}

// Returns the server config or an error for the requested MAC address.
func (s *Spriteful) findServerConfig(macAddress string) (*Server, error) {
	logrus.Infof(`requesting configuration for server "%s".`, macAddress)
	for _, server := range s.Servers {
		if strings.EqualFold(macAddress, server.MacAddress) {
			logrus.Info("configuration found.")
			return &server, nil
		}
	}
	logrus.Warn("configuration not found.")
	return nil, errors.New(fmt.Sprintf("no configuration defined for %s.", macAddress))
}
