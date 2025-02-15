/**
# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package modifier

import (
	"fmt"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/config"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/config/image"
	"github.com/NVIDIA/nvidia-container-toolkit/internal/oci"
	cdi "github.com/container-orchestrated-devices/container-device-interface/pkg/cdi"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

type cdiModifier struct {
	logger   *logrus.Logger
	specDirs []string
	devices  []string
}

// NewCDIModifier creates an OCI spec modifier that determines the modifications to make based on the
// CDI specifications available on the system. The NVIDIA_VISIBLE_DEVICES enviroment variable is
// used to select the devices to include.
func NewCDIModifier(logger *logrus.Logger, cfg *config.Config, ociSpec oci.Spec) (oci.SpecModifier, error) {
	devices, err := getDevicesFromSpec(logger, ociSpec, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get required devices from OCI specification: %v", err)
	}
	if len(devices) == 0 {
		logger.Debugf("No devices requested; no modification required.")
		return nil, nil
	}
	logger.Debugf("Creating CDI modifier for devices: %v", devices)

	specDirs := cdi.DefaultSpecDirs
	if len(cfg.NVIDIAContainerRuntimeConfig.Modes.CDI.SpecDirs) > 0 {
		specDirs = cfg.NVIDIAContainerRuntimeConfig.Modes.CDI.SpecDirs
	}

	m := cdiModifier{
		logger:   logger,
		specDirs: specDirs,
		devices:  devices,
	}

	return m, nil
}

func getDevicesFromSpec(logger *logrus.Logger, ociSpec oci.Spec, cfg *config.Config) ([]string, error) {
	rawSpec, err := ociSpec.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load OCI spec: %v", err)
	}

	_, annotationDevices, err := cdi.ParseAnnotations(rawSpec.Annotations)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container annotations: %v", err)
	}
	if len(annotationDevices) > 0 {
		return annotationDevices, nil
	}

	container, err := image.NewCUDAImageFromSpec(rawSpec)
	if err != nil {
		return nil, err
	}
	envDevices := container.DevicesFromEnvvars(visibleDevicesEnvvar)

	var devices []string
	seen := make(map[string]bool)
	for _, name := range envDevices.List() {
		if !cdi.IsQualifiedName(name) {
			name = fmt.Sprintf("%s=%s", cfg.NVIDIAContainerRuntimeConfig.Modes.CDI.DefaultKind, name)
		}
		if seen[name] {
			logger.Debugf("Ignoring duplicate device %q", name)
			continue
		}
		devices = append(devices, name)
	}

	if len(devices) == 0 {
		return nil, nil
	}

	if cfg.AcceptEnvvarUnprivileged || image.IsPrivileged(rawSpec) {
		return devices, nil
	}

	logger.Warningf("Ignoring devices specified in NVIDIA_VISIBLE_DEVICES: %v", devices)

	return nil, nil
}

// Modify loads the CDI registry and injects the specified CDI devices into the OCI runtime specification.
func (m cdiModifier) Modify(spec *specs.Spec) error {
	registry := cdi.GetRegistry(
		cdi.WithSpecDirs(m.specDirs...),
		cdi.WithAutoRefresh(false),
	)
	if err := registry.Refresh(); err != nil {
		m.logger.Debugf("The following error was triggered when refreshing the CDI registry: %v", err)
	}

	m.logger.Debugf("Injecting devices using CDI: %v", m.devices)
	_, err := registry.InjectDevices(spec, m.devices...)
	if err != nil {
		return fmt.Errorf("failed to inject CDI devices: %v", err)
	}

	return nil
}
