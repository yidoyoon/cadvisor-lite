// Copyright 2016 Google Inc. All Rights Reserved.
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

// Provides global docker information.
package docker

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"golang.org/x/net/context"

	"github.com/yidoyoon/cadvisor-lite/container/docker/utils"
	v1 "github.com/yidoyoon/cadvisor-lite/info/v1"
	"github.com/yidoyoon/cadvisor-lite/machine"
)

var dockerTimeout = 10 * time.Second

func defaultContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), dockerTimeout)
	return ctx
}

func SetTimeout(timeout time.Duration) {
	dockerTimeout = timeout
}

func Status() (v1.DockerStatus, error) {
	return StatusWithContext(defaultContext())
}

func StatusWithContext(ctx context.Context) (v1.DockerStatus, error) {
	client, err := Client()
	if err != nil {
		return v1.DockerStatus{}, fmt.Errorf("unable to communicate with docker daemon: %v", err)
	}
	dockerInfo, err := client.Info(ctx)
	if err != nil {
		return v1.DockerStatus{}, err
	}
	return StatusFromDockerInfo(dockerInfo)
}

func StatusFromDockerInfo(dockerInfo dockertypes.Info) (v1.DockerStatus, error) {
	out := v1.DockerStatus{}
	out.KernelVersion = machine.KernelVersion()
	out.OS = dockerInfo.OperatingSystem
	out.Hostname = dockerInfo.Name
	out.RootDir = dockerInfo.DockerRootDir
	out.Driver = dockerInfo.Driver
	out.NumImages = dockerInfo.Images
	out.NumContainers = dockerInfo.Containers
	out.DriverStatus = make(map[string]string, len(dockerInfo.DriverStatus))
	for _, v := range dockerInfo.DriverStatus {
		out.DriverStatus[v[0]] = v[1]
	}
	var err error
	ver, err := VersionString()
	if err != nil {
		return out, err
	}
	out.Version = ver
	ver, err = APIVersionString()
	if err != nil {
		return out, err
	}
	out.APIVersion = ver
	return out, nil
}

func Images() ([]v1.DockerImage, error) {
	client, err := Client()
	if err != nil {
		return nil, fmt.Errorf("unable to communicate with docker daemon: %v", err)
	}
	summaries, err := client.ImageList(defaultContext(), dockertypes.ImageListOptions{All: false})
	if err != nil {
		return nil, err
	}
	return utils.SummariesToImages(summaries)
}

// Checks whether the dockerInfo reflects a valid docker setup, and returns it if it does, or an
// error otherwise.
func ValidateInfo(GetInfo func() (*dockertypes.Info, error), ServerVersion func() (string, error)) (*dockertypes.Info, error) {
	info, err := GetInfo()
	if err != nil {
		return nil, err
	}

	// Fall back to version API if ServerVersion is not set in info.
	if info.ServerVersion == "" {
		var err error
		info.ServerVersion, err = ServerVersion()
		if err != nil {
			return nil, fmt.Errorf("unable to get runtime version: %v", err)
		}
	}

	version, err := ParseVersion(info.ServerVersion, VersionRe, 3)
	if err != nil {
		return nil, err
	}

	if version[0] < 1 {
		return nil, fmt.Errorf("cAdvisor requires runtime version %v or above but we have found version %v reported as %q", []int{1, 0, 0}, version, info.ServerVersion)
	}

	if info.Driver == "" {
		return nil, fmt.Errorf("failed to find runtime storage driver")
	}

	return info, nil
}

func Info() (*dockertypes.Info, error) {
	client, err := Client()
	if err != nil {
		return nil, fmt.Errorf("unable to communicate with docker daemon: %v", err)
	}

	dockerInfo, err := client.Info(defaultContext())
	if err != nil {
		return nil, fmt.Errorf("failed to detect Docker info: %v", err)
	}

	return &dockerInfo, nil
}

func APIVersion() ([]int, error) {
	ver, err := APIVersionString()
	if err != nil {
		return nil, err
	}
	return ParseVersion(ver, apiVersionRe, 2)
}

func VersionString() (string, error) {
	dockerVersion := "Unknown"
	client, err := Client()
	if err == nil {
		version, err := client.ServerVersion(defaultContext())
		if err == nil {
			dockerVersion = version.Version
		}
	}
	return dockerVersion, err
}

func APIVersionString() (string, error) {
	apiVersion := "Unknown"
	client, err := Client()
	if err == nil {
		version, err := client.ServerVersion(defaultContext())
		if err == nil {
			apiVersion = version.APIVersion
		}
	}
	return apiVersion, err
}

func ParseVersion(versionString string, regex *regexp.Regexp, length int) ([]int, error) {
	matches := regex.FindAllStringSubmatch(versionString, -1)
	if len(matches) != 1 {
		return nil, fmt.Errorf("version string \"%v\" doesn't match expected regular expression: \"%v\"", versionString, regex.String())
	}
	versionStringArray := matches[0][1:]
	versionArray := make([]int, length)
	for index, versionStr := range versionStringArray {
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, fmt.Errorf("error while parsing \"%v\" in \"%v\"", versionStr, versionString)
		}
		versionArray[index] = version
	}
	return versionArray, nil
}
