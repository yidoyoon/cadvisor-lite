// Copyright 2014 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package benchframework

import (
	"bytes"
	"flag"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"k8s.io/klog/v2"

	"github.com/yidoyoon/cadvisor-lite/client"
	v2 "github.com/yidoyoon/cadvisor-lite/client/v2"
)

var host = flag.String("host", "localhost", "Address of the host being tested")
var port = flag.Int("port", 8080, "Port of the application on the host being tested")
var sshOptions = flag.String("ssh-options", "", "Command line options for ssh")

// Integration bench framework.
type Framework interface {
	// Clean the framework state.
	Cleanup()

	// The testing.T used by the framework and the current test.
	B() *testing.B

	// Returns the hostname being tested.
	Hostname() HostnameInfo

	// Returns the Docker actions for the test framework.
	Docker() DockerActions

	// Returns the shell actions for the test framework.
	Shell() ShellActions

	// Returns the cAdvisor actions for the test framework.
	Cadvisor() CadvisorActions
}

// Instantiates a Framework. Cleanup *must* be called. Class is thread-compatible.
// All framework actions report fatal errors on the t specified at creation time.
//
// Typical use:
//
//	func TestFoo(t *testing.T) {
//		fm := framework.New(t)
//		defer fm.Cleanup()
//	     ... actual test ...
//	}
func New(b *testing.B) Framework {
	// All integration tests are large.
	if testing.Short() {
		b.Skip("Skipping framework test in short mode")
	}

	// Try to see if non-localhost hosts are GCE instances.
	bm := &realFramework{
		hostname: HostnameInfo{
			Host: *host,
			Port: *port,
		},
		b:        b,
		cleanups: make([]func(), 0),
	}
	bm.shellActions = shellActions{
		fm: bm,
	}
	bm.dockerActions = dockerActions{
		fm: bm,
	}

	return bm
}

const (
	Aufs         string = "aufs"
	Overlay      string = "overlay"
	Overlay2     string = "overlay2"
	DeviceMapper string = "devicemapper"
	Unknown      string = ""
)

type DockerActions interface {
	// Run the no-op pause Docker container and return its ID.
	RunPause() string

	// Run the specified command in a Docker busybox container and return its ID.
	RunBusybox(cmd ...string) string

	// Runs a Docker container in the background. Uses the specified DockerRunArgs and command.
	// Returns the ID of the new container.
	//
	// e.g.:
	// Run(DockerRunArgs{Image: "busybox"}, "ping", "www.google.com")
	//   -> docker run busybox ping www.google.com
	Run(args DockerRunArgs, cmd ...string) string
	RunStress(args DockerRunArgs, cmd ...string) string

	Version() []string
	StorageDriver() string
}

type ShellActions interface {
	// Runs a specified command and arguments. Returns the stdout and stderr.
	Run(cmd string, args ...string) (string, string)
	RunStress(cmd string, args ...string) (string, string)
}

type CadvisorActions interface {
	// Returns a cAdvisor client to the machine being tested.
	Client() *client.Client
	ClientV2() *v2.Client
}

type realFramework struct {
	hostname         HostnameInfo
	b                *testing.B
	cadvisorClient   *client.Client
	cadvisorClientV2 *v2.Client

	shellActions  shellActions
	dockerActions dockerActions

	// Cleanup functions to call on Cleanup()
	cleanups []func()
}

type shellActions struct {
	fm *realFramework
}

type dockerActions struct {
	fm *realFramework
}

type HostnameInfo struct {
	Host string
	Port int
}

// Returns: http://<host>:<port>/
func (h HostnameInfo) FullHostname() string {
	return fmt.Sprintf("http://%s:%d/", h.Host, h.Port)
}

func (f *realFramework) B() *testing.B {
	return f.b
}

func (f *realFramework) Hostname() HostnameInfo {
	return f.hostname
}

func (f *realFramework) Shell() ShellActions {
	return f.shellActions
}

func (f *realFramework) Docker() DockerActions {
	return f.dockerActions
}

func (f *realFramework) Cadvisor() CadvisorActions {
	return f
}

// Call all cleanup functions.
func (f *realFramework) Cleanup() {
	for _, cleanupFunc := range f.cleanups {
		cleanupFunc()
	}
}

// Gets a client to the cAdvisor being tested.
func (f *realFramework) Client() *client.Client {
	if f.cadvisorClient == nil {
		cadvisorClient, err := client.NewClient(f.Hostname().FullHostname())
		if err != nil {
			f.b.Fatalf("Failed to instantiate the cAdvisor client: %v", err)
		}
		f.cadvisorClient = cadvisorClient
	}
	return f.cadvisorClient
}

// Gets a v2 client to the cAdvisor being tested.
func (f *realFramework) ClientV2() *v2.Client {
	if f.cadvisorClientV2 == nil {
		cadvisorClientV2, err := v2.NewClient(f.Hostname().FullHostname())
		if err != nil {
			f.b.Fatalf("Failed to instantiate the cAdvisor client: %v", err)
		}
		f.cadvisorClientV2 = cadvisorClientV2
	}
	return f.cadvisorClientV2
}

func (a dockerActions) RunPause() string {
	return a.Run(DockerRunArgs{
		Image: "registry.k8s.io/pause",
	})
}

// Run the specified command in a Docker busybox container.
func (a dockerActions) RunBusybox(cmd ...string) string {
	return a.Run(DockerRunArgs{
		Image: "busybox",
	}, cmd...)
}

type DockerRunArgs struct {
	// Image to use.
	Image string

	// Arguments to the Docker CLI.
	Args []string

	InnerArgs []string
}

// TODO(vmarmol): Use the Docker remote API.
// TODO(vmarmol): Refactor a set of "RunCommand" actions.
// Runs a Docker container in the background. Uses the specified DockerRunArgs and command.
//
// e.g.:
// RunDockerContainer(DockerRunArgs{Image: "busybox"}, "ping", "www.google.com")
//
//	-> docker run busybox ping www.google.com
func (a dockerActions) Run(args DockerRunArgs, cmd ...string) string {
	dockerCommand := append(append([]string{"docker", "run", "-d"}, args.Args...), args.Image)
	dockerCommand = append(dockerCommand, cmd...)
	output, _ := a.fm.Shell().Run("sudo", dockerCommand...)

	// The last line is the container ID.
	elements := strings.Fields(output)
	containerID := elements[len(elements)-1]

	a.fm.cleanups = append(a.fm.cleanups, func() {
		a.fm.Shell().Run("sudo", "docker", "rm", "-f", containerID)
	})
	return containerID
}
func (a dockerActions) Version() []string {
	dockerCommand := []string{"docker", "version", "-f", "'{{.Server.Version}}'"}
	output, _ := a.fm.Shell().Run("sudo", dockerCommand...)
	output = strings.TrimSpace(output)
	ret := strings.Split(output, ".")
	if len(ret) != 3 {
		a.fm.B().Fatalf("invalid version %v", output)
	}
	return ret
}

func (a dockerActions) StorageDriver() string {
	dockerCommand := []string{"docker", "info"}
	output, _ := a.fm.Shell().Run("sudo", dockerCommand...)
	if len(output) < 1 {
		a.fm.B().Fatalf("failed to find docker storage driver - %v", output)
	}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Storage Driver: ") {
			idx := strings.LastIndex(line, ": ") + 2
			driver := line[idx:]
			switch driver {
			case Aufs, Overlay, Overlay2, DeviceMapper:
				return driver
			default:
				return Unknown
			}
		}
	}
	a.fm.B().Fatalf("failed to find docker storage driver from info - %v", output)
	return Unknown
}

func (a dockerActions) RunStress(args DockerRunArgs, cmd ...string) string {
	dockerCommand := append(append(append(append([]string{"docker", "run", "-m=4M", "-d", "-t", "-i"}, args.Args...), args.Image), args.InnerArgs...), cmd...)

	output, _ := a.fm.Shell().RunStress("sudo", dockerCommand...)

	// The last line is the container ID.
	if len(output) < 1 {
		a.fm.B().Fatalf("need 1 arguments in output %v to get the name but have %v", output, len(output))
	}
	elements := strings.Fields(output)
	containerID := elements[len(elements)-1]

	a.fm.cleanups = append(a.fm.cleanups, func() {
		a.fm.Shell().Run("sudo", "docker", "rm", "-f", containerID)
	})
	return containerID
}

func (a shellActions) wrapSSH(command string, args ...string) *exec.Cmd {
	cmd := []string{a.fm.Hostname().Host, "--", "sh", "-c", "\"", command}
	cmd = append(cmd, args...)
	cmd = append(cmd, "\"")
	if *sshOptions != "" {
		cmd = append(strings.Split(*sshOptions, " "), cmd...)
	}
	return exec.Command("ssh", cmd...)
}

func (a shellActions) Run(command string, args ...string) (string, string) {
	var cmd *exec.Cmd
	if a.fm.Hostname().Host == "localhost" {
		// Just run locally.
		cmd = exec.Command(command, args...)
	} else {
		// We must SSH to the remote machine and run the command.
		cmd = a.wrapSSH(command, args...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	klog.Infof("About to run - %v", cmd.Args)
	err := cmd.Run()
	if err != nil {
		a.fm.B().Fatalf("Failed to run %q %v in %q with error: %q. Stdout: %q, Stderr: %s", command, args, a.fm.Hostname().Host, err, stdout.String(), stderr.String())
		return "", ""
	}
	return stdout.String(), stderr.String()
}

func (a shellActions) RunStress(command string, args ...string) (string, string) {
	var cmd *exec.Cmd
	if a.fm.Hostname().Host == "localhost" {
		// Just run locally.
		cmd = exec.Command(command, args...)
	} else {
		// We must SSH to the remote machine and run the command.
		cmd = a.wrapSSH(command, args...)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		a.fm.B().Logf("Ran %q %v in %q and received error: %q. Stdout: %q, Stderr: %s", command, args, a.fm.Hostname().Host, err, stdout.String(), stderr.String())
		return stdout.String(), stderr.String()
	}
	return stdout.String(), stderr.String()
}

// Runs retryFunc until no error is returned. After dur time the last error is returned.
// Note that the function does not timeout the execution of retryFunc when the limit is reached.
func RetryForDuration(retryFunc func() error, dur time.Duration) error {
	waitUntil := time.Now().Add(dur)
	var err error
	for time.Now().Before(waitUntil) {
		err = retryFunc()
		if err == nil {
			return nil
		}
	}
	return err
}
