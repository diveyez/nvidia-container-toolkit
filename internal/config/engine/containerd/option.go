/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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

package containerd

import (
	"fmt"
	"os"

	"github.com/NVIDIA/nvidia-container-toolkit/internal/config/engine"
	"github.com/pelletier/go-toml"
	log "github.com/sirupsen/logrus"
)

const (
	defaultRuntimeType = "io.containerd.runc.v2"
)

type builder struct {
	path            string
	runtimeType     string
	useLegacyConfig bool
}

// Option defines a function that can be used to configure the config builder
type Option func(*builder)

// WithPath sets the path for the config builder
func WithPath(path string) Option {
	return func(b *builder) {
		b.path = path
	}
}

// WithRuntimeType sets the runtime type for the config builder
func WithRuntimeType(runtimeType string) Option {
	return func(b *builder) {
		b.runtimeType = runtimeType
	}
}

// WithUseLegacyConfig sets the useLegacyConfig flag for the config builder
func WithUseLegacyConfig(useLegacyConfig bool) Option {
	return func(b *builder) {
		b.useLegacyConfig = useLegacyConfig
	}
}

func (b *builder) build() (engine.Interface, error) {
	if b.path == "" {
		return nil, fmt.Errorf("config path is empty")
	}

	if b.runtimeType == "" {
		b.runtimeType = defaultRuntimeType
	}

	config, err := loadConfig(b.path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %v", err)
	}
	config.RuntimeType = b.runtimeType
	config.UseDefaultRuntimeName = !b.useLegacyConfig

	version, err := config.parseVersion(b.useLegacyConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config version: %v", err)
	}
	switch version {
	case 1:
		return (*ConfigV1)(config), nil
	case 2:
		return config, nil
	}

	return nil, fmt.Errorf("unsupported config version: %v", version)
}

// loadConfig loads the containerd config from disk
func loadConfig(config string) (*Config, error) {
	log.Infof("Loading config: %v", config)

	info, err := os.Stat(config)
	if os.IsExist(err) && info.IsDir() {
		return nil, fmt.Errorf("config file is a directory")
	}

	configFile := config
	if os.IsNotExist(err) {
		configFile = "/dev/null"
		log.Infof("Config file does not exist, creating new one")
	}

	tomlConfig, err := toml.LoadFile(configFile)
	if err != nil {
		return nil, err
	}

	log.Infof("Successfully loaded config")

	cfg := Config{
		Tree: tomlConfig,
	}
	return &cfg, nil
}

// parseVersion returns the version of the config
func (c *Config) parseVersion(useLegacyConfig bool) (int, error) {
	defaultVersion := 2
	if useLegacyConfig {
		defaultVersion = 1
	}

	switch v := c.Get("version").(type) {
	case nil:
		switch len(c.Keys()) {
		case 0: // No config exists, or the config file is empty, use version inferred from containerd
			return defaultVersion, nil
		default: // A config file exists, has content, and no version is set
			return 1, nil
		}
	case int64:
		return int(v), nil
	default:
		return -1, fmt.Errorf("unsupported type for version field: %v", v)
	}
}
