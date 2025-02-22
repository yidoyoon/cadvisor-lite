// Copyright 2015 Google Inc. All Rights Reserved.
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

package api

import (
	"fmt"
	"net/http"
	"path"
	"strconv"
	"time"

	_ "github.com/hodgesds/perf-utils"
	info "github.com/yidoyoon/cadvisor-lite/info/v1"
	v2 "github.com/yidoyoon/cadvisor-lite/info/v2"
	"github.com/yidoyoon/cadvisor-lite/manager"

	"k8s.io/klog/v2"
)

const (
	containersAPI    = "containers"
	subcontainersAPI = "subcontainers"
	machineAPI       = "machine"
	machineStatsAPI  = "machinestats"
	dockerAPI        = "docker"
	summaryAPI       = "summary"
	statsAPI         = "stats"
	specAPI          = "spec"
	eventsAPI        = "events"
	storageAPI       = "storage"
	attributesAPI    = "attributes"
	versionAPI       = "version"
	psAPI            = "ps"
	customMetricsAPI = "appmetrics"
)

// Interface for a cAdvisor API version
type ApiVersion interface {
	// Returns the version string.
	Version() string

	// List of supported API endpoints.
	SupportedRequestTypes() []string

	// Handles a request. The second argument is the parameters after /api/<version>/<endpoint>
	HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error
}

// Gets all supported API versions.
func getAPIVersions() []ApiVersion {
	v1_0 := &version1_0{}
	v1_1 := newVersion1_1(v1_0)
	v1_2 := newVersion1_2(v1_1)
	v1_3 := newVersion1_3(v1_2)
	v2_0 := newVersion2_0()
	v2_1 := newVersion2_1(v2_0)

	return []ApiVersion{v1_0, v1_1, v1_2, v1_3, v2_0, v2_1}

}

// API v1.0

type version1_0 struct {
}

func (api *version1_0) Version() string {
	return "v1.0"
}

func (api *version1_0) SupportedRequestTypes() []string {
	return []string{containersAPI, machineAPI}
}

func (api *version1_0) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	switch requestType {
	case machineAPI:
		klog.V(4).Infof("Api - Machine")

		// Get the MachineInfo
		machineInfo, err := m.GetMachineInfo()
		if err != nil {
			return err
		}

		err = writeResult(machineInfo, w)
		if err != nil {
			return err
		}
	case containersAPI:
		containerName := getContainerName(request)
		klog.V(4).Infof("Api - Container(%s)", containerName)

		// Get the query request.
		query, err := getContainerInfoRequest(r.Body)
		if err != nil {
			return err
		}

		// Get the container.
		cont, err := m.GetContainerInfo(containerName, query)
		if err != nil {
			return fmt.Errorf("failed to get container %q with error: %s", containerName, err)
		}

		// Only output the container as JSON.
		err = writeResult(cont, w)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown request type %q", requestType)
	}
	return nil
}

// API v1.1

type version1_1 struct {
	baseVersion *version1_0
}

// v1.1 builds on v1.0.
func newVersion1_1(v *version1_0) *version1_1 {
	return &version1_1{
		baseVersion: v,
	}
}

func (api *version1_1) Version() string {
	return "v1.1"
}

func (api *version1_1) SupportedRequestTypes() []string {
	return append(api.baseVersion.SupportedRequestTypes(), subcontainersAPI)
}

func (api *version1_1) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	switch requestType {
	case subcontainersAPI:
		containerName := getContainerName(request)
		klog.V(4).Infof("Api - Subcontainers(%s)", containerName)

		// Get the query request.
		query, err := getContainerInfoRequest(r.Body)
		if err != nil {
			return err
		}

		// Get the subcontainers.
		containers, err := m.SubcontainersInfo(containerName, query)
		if err != nil {
			return fmt.Errorf("failed to get subcontainers for container %q with error: %s", containerName, err)
		}

		// Only output the containers as JSON.
		err = writeResult(containers, w)
		if err != nil {
			return err
		}
		return nil
	default:
		return api.baseVersion.HandleRequest(requestType, request, m, w, r)
	}
}

// API v1.2

type version1_2 struct {
	baseVersion *version1_1
}

// v1.2 builds on v1.1.
func newVersion1_2(v *version1_1) *version1_2 {
	return &version1_2{
		baseVersion: v,
	}
}

func (api *version1_2) Version() string {
	return "v1.2"
}

func (api *version1_2) SupportedRequestTypes() []string {
	return append(api.baseVersion.SupportedRequestTypes(), dockerAPI)
}

func (api *version1_2) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	switch requestType {
	case dockerAPI:
		klog.V(4).Infof("Api - Docker(%v)", request)

		// Get the query request.
		query, err := getContainerInfoRequest(r.Body)
		if err != nil {
			return err
		}

		var containers map[string]info.ContainerInfo
		// map requests for "docker/" to "docker"
		if len(request) == 1 && len(request[0]) == 0 {
			request = request[:0]
		}
		switch len(request) {
		case 0:
			// Get all Docker containers.
			containers, err = m.AllDockerContainers(query)
			if err != nil {
				return fmt.Errorf("failed to get all Docker containers with error: %v", err)
			}
		case 1:
			// Get one Docker container.
			var cont info.ContainerInfo
			cont, err = m.DockerContainer(request[0], query)
			if err != nil {
				return fmt.Errorf("failed to get Docker container %q with error: %v", request[0], err)
			}
			containers = map[string]info.ContainerInfo{
				cont.Name: cont,
			}
		default:
			return fmt.Errorf("unknown request for Docker container %v", request)
		}

		// Only output the containers as JSON.
		err = writeResult(containers, w)
		if err != nil {
			return err
		}
		return nil
	default:
		return api.baseVersion.HandleRequest(requestType, request, m, w, r)
	}
}

// API v1.3

type version1_3 struct {
	baseVersion *version1_2
}

// v1.3 builds on v1.2.
func newVersion1_3(v *version1_2) *version1_3 {
	return &version1_3{
		baseVersion: v,
	}
}

func (api *version1_3) Version() string {
	return "v1.3"
}

func (api *version1_3) SupportedRequestTypes() []string {
	return append(api.baseVersion.SupportedRequestTypes(), eventsAPI)
}

func (api *version1_3) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	switch requestType {
	case eventsAPI:
		return handleEventRequest(request, m, w, r)
	default:
		return api.baseVersion.HandleRequest(requestType, request, m, w, r)
	}
}

func handleEventRequest(request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	query, stream, err := getEventRequest(r)
	if err != nil {
		return err
	}
	query.ContainerName = path.Join("/", getContainerName(request))
	klog.V(4).Infof("Api - Events(%v)", query)
	if !stream {
		pastEvents, err := m.GetPastEvents(query)
		if err != nil {
			return err
		}
		return writeResult(pastEvents, w)
	}
	eventChannel, err := m.WatchForEvents(query)
	if err != nil {
		return err
	}
	return streamResults(eventChannel, w, r, m)

}

// API v2.0

type version2_0 struct {
}

func newVersion2_0() *version2_0 {
	return &version2_0{}
}

func (api *version2_0) Version() string {
	return "v2.0"
}

func (api *version2_0) SupportedRequestTypes() []string {
	return []string{versionAPI, attributesAPI, eventsAPI, machineAPI, summaryAPI, statsAPI, specAPI, storageAPI, psAPI, customMetricsAPI}
}

func (api *version2_0) handleStatsAPI(request []string, opt v2.RequestOptions, m manager.Manager, w http.ResponseWriter) error {
	name := getContainerName(request)

	klog.V(4).Infof("Api - Stats: Looking for stats for container %q, options %+v", name, opt)
	infos, err := m.GetRequestedContainersInfo(name, opt)
	if err != nil {
		if len(infos) == 0 {
			return err
		}
		klog.Errorf("Error calling GetRequestedContainersInfo: %v", err)
	}
	contStats := make(map[string][]v2.DeprecatedContainerStats)
	for name, cinfo := range infos {
		contStats[name] = v2.DeprecatedStatsFromV1(cinfo)
	}

	return writeResult(contStats, w)
}

func (api *version2_0) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	opt, err := GetRequestOptions(r)
	if err != nil {
		return err
	}
	switch requestType {
	case statsAPI:
		//errorWrapper := func() error {
		//	return api.handleStatsAPI(request, opt, m, w)
		//}

		//cpuInstructions, _ := perf.CPUInstructions(errorWrapper)
		//cpuCycles, _ := perf.CPUCycles(errorWrapper)
		//cacheRef, _ := perf.CacheRef(errorWrapper)
		//cacheMiss, _ := perf.CacheMiss(errorWrapper)
		//cpuRefCycles, _ := perf.CPURefCycles(errorWrapper)
		//cpuClock, _ := perf.CPUClock(errorWrapper)
		//cpuTaskClock, _ := perf.CPUTaskClock(errorWrapper)
		//pageFaults, _ := perf.PageFaults(errorWrapper)
		//contextSwitches, _ := perf.ContextSwitches(errorWrapper)
		//minorPageFaults, _ := perf.MinorPageFaults(errorWrapper)
		//majorPageFaults, _ := perf.MajorPageFaults(errorWrapper)
		//
		//fmt.Println(cpuInstructions.Value, cpuCycles.Value, cacheRef.Value, cacheMiss.Value, cpuRefCycles.Value, cpuClock.Value, cpuTaskClock.Value, pageFaults.Value, contextSwitches.Value, minorPageFaults.Value, majorPageFaults.Value)

		return api.handleStatsAPI(request, opt, m, w)
	default:
		return fmt.Errorf("unknown request type %q", requestType)
	}
}

//func (api *version2_0) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
//	opt, err := GetRequestOptions(r)
//	if err != nil {
//		return err
//	}
//	switch requestType {
//	case statsAPI:
//		errorWrapper := func() error {
//			return api.handleStatsAPI(request, opt, m, w)
//		}
//
//		fmt.Printf("-----------------------------------\n")
//		perfFuncs := map[string]func(func() error) (*perf.ProfileValue, error){
//			"CPU instructions":  perf.CPUInstructions,
//			"CPU cycles":        perf.CPUCycles,
//			"Cache ref":         perf.CacheRef,
//			"Cache miss":        perf.CacheMiss,
//			"CPU ref cycles":    perf.CPURefCycles,
//			"CPU clock":         perf.CPUClock,
//			"CPU task clock":    perf.CPUTaskClock,
//			"Page faults":       perf.PageFaults,
//			"Context switches":  perf.ContextSwitches,
//			"Minor page faults": perf.MinorPageFaults,
//			"Major page faults": perf.MajorPageFaults,
//		}
//		keys := make([]string, 0, len(perfFuncs))
//		for k := range perfFuncs {
//			keys = append(keys, k)
//		}
//		sort.Strings(keys)
//		var metricsLine string
//		for _, k := range keys {
//			perfFunc := perfFuncs[k]
//			profileValue, err := perfFunc(errorWrapper)
//			if err != nil {
//				log.Fatal(err)
//			}
//			metricsLine += fmt.Sprintf("%s: %v, ", k, profileValue.Value)
//		}
//		fmt.Println(metricsLine)
//
//		return api.handleStatsAPI(request, opt, m, w)
//	default:
//		return fmt.Errorf("unknown request type %q", requestType)
//	}
//}

type version2_1 struct {
	baseVersion *version2_0
}

func newVersion2_1(v *version2_0) *version2_1 {
	return &version2_1{
		baseVersion: v,
	}
}

func (api *version2_1) Version() string {
	return "v2.1"
}

func (api *version2_1) SupportedRequestTypes() []string {
	return append([]string{machineStatsAPI}, api.baseVersion.SupportedRequestTypes()...)
}

func (api *version2_1) HandleRequest(requestType string, request []string, m manager.Manager, w http.ResponseWriter, r *http.Request) error {
	// Get the query request.
	opt, err := GetRequestOptions(r)
	if err != nil {
		return err
	}

	switch requestType {
	case machineStatsAPI:
		klog.V(4).Infof("Api - MachineStats(%v)", request)
		cont, err := m.GetRequestedContainersInfo("/", opt)
		if err != nil {
			if len(cont) == 0 {
				return err
			}
			klog.Errorf("Error calling GetRequestedContainersInfo: %v", err)
		}
		return writeResult(v2.MachineStatsFromV1(cont["/"]), w)
	case statsAPI:
		name := getContainerName(request)
		klog.V(4).Infof("Api - Stats: Looking for stats for container %q, options %+v", name, opt)
		conts, err := m.GetRequestedContainersInfo(name, opt)
		if err != nil {
			if len(conts) == 0 {
				return err
			}
			klog.Errorf("Error calling GetRequestedContainersInfo: %v", err)
		}
		contStats := make(map[string]v2.ContainerInfo, len(conts))
		for name, cont := range conts {
			if name == "/" {
				// Root cgroup stats should be exposed as machine stats
				continue
			}
			contStats[name] = v2.ContainerInfo{
				Spec:  v2.ContainerSpecFromV1(&cont.Spec, cont.Aliases, cont.Namespace),
				Stats: v2.ContainerStatsFromV1(name, &cont.Spec, cont.Stats),
			}
		}
		return writeResult(contStats, w)
	default:
		return api.baseVersion.HandleRequest(requestType, request, m, w, r)
	}
}

// GetRequestOptions returns the metrics request options from a HTTP request.
func GetRequestOptions(r *http.Request) (v2.RequestOptions, error) {
	supportedTypes := map[string]bool{
		v2.TypeName:   true,
		v2.TypeDocker: true,
		v2.TypePodman: true,
	}
	// fill in the defaults.
	opt := v2.RequestOptions{
		IdType:    v2.TypeName,
		Count:     64,
		Recursive: false,
	}
	idType := r.URL.Query().Get("type")
	if len(idType) != 0 {
		if !supportedTypes[idType] {
			return opt, fmt.Errorf("unknown 'type' %q", idType)
		}
		opt.IdType = idType
	}
	count := r.URL.Query().Get("count")
	if len(count) != 0 {
		n, err := strconv.Atoi(count)
		if err != nil {
			return opt, fmt.Errorf("failed to parse 'count' option: %v", count)
		}
		if n < -1 {
			return opt, fmt.Errorf("invalid 'count' option: only -1 and larger values allowed, not %d", n)
		}
		opt.Count = n
	}
	recursive := r.URL.Query().Get("recursive")
	if recursive == "true" {
		opt.Recursive = true
	}
	if maxAgeString := r.URL.Query().Get("max_age"); len(maxAgeString) > 0 {
		maxAge, err := time.ParseDuration(maxAgeString)
		if err != nil {
			return opt, fmt.Errorf("failed to parse 'max_age' option: %v", err)
		}
		opt.MaxAge = &maxAge
	}
	return opt, nil
}
