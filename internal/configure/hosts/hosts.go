/*
 *  Copyright (c) 2022 NetEase Inc.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

/*
 * Project: CurveAdm
 * Created Date: 2022-07-20
 * Author: Jingli Chen (Wine93)
 */

// __SIGN_BY_WINE93__

package hosts

import (
	"bytes"
	"github.com/opencurve/curveadm/pkg/variable"
	"strings"

	"github.com/opencurve/curveadm/internal/build"
	"github.com/opencurve/curveadm/internal/configure/os"
	"github.com/opencurve/curveadm/internal/errno"
	"github.com/opencurve/curveadm/internal/utils"
	log "github.com/opencurve/curveadm/pkg/log/glg"
	"github.com/spf13/viper"
)

const (
	KEY_LABELS    = "labels"
	KEY_ENVS      = "envs"
	KEY_INSTANCES = "instances"

	PERMISSIONS_600 = 384 // -rw------- (256 + 128 = 384)
)

type (
	Hosts struct {
		Global map[string]interface{}   `mapstructure:"global"`
		Host   []map[string]interface{} `mapstructure:"hosts"`
	}

	HostConfig struct {
		sequence  int
		config    map[string]interface{}
		labels    []string
		envs      []string
		variables *variable.Variables
		//instances and instancesSequence only used in the memcached deploy
		//instances is the num of memcached servers will be deployed in the same host
		instances int
		//instancesSquence is the sequence num of memcached servers in the same host
		instancesSequence int
	}
)

func newIfNil(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return map[string]interface{}{}
	}
	return config
}

func merge(parent, child map[string]interface{}) {
	for k, v := range parent {
		if child[k] == nil {
			child[k] = v
		}
	}
}

func (hc *HostConfig) convertLables() error {
	value := hc.config[KEY_LABELS]
	slice, ok := (value).([]interface{})
	if !ok {
		return errno.ERR_CONFIGURE_VALUE_REQUIRES_STRING_SLICE.
			F("hosts[%d].%s = %v", hc.sequence, KEY_LABELS, value)
	}

	for _, value := range slice {
		if v, ok := utils.All2Str(value); !ok {
			return errno.ERR_CONFIGURE_VALUE_REQUIRES_STRING_SLICE.
				F("hosts[%d].%s = %v", hc.sequence, KEY_LABELS, value)
		} else {
			hc.labels = append(hc.labels, v)
		}
	}

	return nil
}

func (hc *HostConfig) convertEnvs() error {
	value := hc.config[KEY_ENVS]
	slice, ok := (value).([]interface{})
	if !ok {
		return errno.ERR_CONFIGURE_VALUE_REQUIRES_STRING_SLICE.
			F("hosts[%d].%s = %v", hc.sequence, KEY_ENVS, value)
	}

	for _, value := range slice {
		if v, ok := utils.All2Str(value); !ok {
			return errno.ERR_CONFIGURE_VALUE_REQUIRES_STRING_SLICE.
				F("hosts[%d].%s = %v", hc.sequence, KEY_ENVS, value)
		} else {
			hc.envs = append(hc.envs, v)
		}
	}

	return nil
}

// read the instances value from hc.config
func (hc *HostConfig) convertInstances() error {
	value := hc.config[KEY_INSTANCES]
	v, ok := utils.All2Str(value)
	if !ok {
		if !utils.IsAnySlice(value) {
			return errno.ERR_UNSUPPORT_CONFIGURE_VALUE_TYPE.
				F("hosts[%d].%s = %v", hc.sequence, KEY_INSTANCES, value)
		}
	}
	if v, ok := utils.Str2Int(v); !ok {
		return errno.ERR_CONFIGURE_VALUE_REQUIRES_INTEGER.
			F("hosts[%d].%s = %v", hc.sequence, KEY_INSTANCES, value)
	} else if v <= 0 {
		return errno.ERR_CONFIGURE_VALUE_REQUIRES_POSITIVE_INTEGER.
			F("hosts[%d].%s = %v", hc.sequence, KEY_INSTANCES, value)
	} else {
		hc.instances = v
		return nil
	}
}

// convert config item to its require type after rendering,
// return error if convert failed
func (hc *HostConfig) convert() error {
	for _, item := range itemset.getAll() {
		k := item.key
		value := hc.get(item) // return config value or default value
		if value == nil {
			continue
		}
		v, ok := utils.All2Str(value)
		if !ok {
			return errno.ERR_UNSUPPORT_CONFIGURE_VALUE_TYPE.
				F("%s: %v", k, value)
		}

		switch item.require {
		case REQUIRE_ANY:
			// do nothing
		case REQUIRE_INT:
			if intv, ok := utils.Str2Int(v); !ok {
				return errno.ERR_CONFIGURE_VALUE_REQUIRES_INTEGER.
					F("%s: %v", k, value)
			} else {
				hc.config[k] = intv
			}
		case REQUIRE_STRING:
			if len(v) == 0 {
				return errno.ERR_CONFIGURE_VALUE_REQUIRES_NON_EMPTY_STRING.
					F("%s: %v", k, value)
			}
		case REQUIRE_BOOL:
			if boolv, ok := utils.Str2Bool(v); !ok {
				return errno.ERR_CONFIGURE_VALUE_REQUIRES_BOOL.
					F("%s: %v", k, value)
			} else {
				hc.config[k] = boolv
			}
		case REQUIRE_POSITIVE_INTEGER:
			if intv, ok := utils.Str2Int(v); !ok {
				return errno.ERR_CONFIGURE_VALUE_REQUIRES_INTEGER.
					F("%s: %v", k, value)
			} else if intv <= 0 {
				return errno.ERR_CONFIGURE_VALUE_REQUIRES_POSITIVE_INTEGER.
					F("%s: %v", k, value)
			} else {
				hc.config[k] = intv
			}
		}
	}
	return nil
}

func (hc *HostConfig) Build() error {
	for key, value := range hc.config {
		if key == KEY_LABELS { // convert labels
			if err := hc.convertLables(); err != nil {
				return err
			}
			hc.config[key] = nil // delete labels section
			continue
		} else if key == KEY_ENVS { // convert envs
			if err := hc.convertEnvs(); err != nil {
				return err
			}
			hc.config[key] = nil // delete envs section
			continue
		} else if key == KEY_INSTANCES { // convert instances
			if err := hc.convertInstances(); err != nil {
				return err
			}
			hc.config[key] = nil // delete instances section
			continue
		}

		if itemset.get(key) == nil {
			return errno.ERR_UNSUPPORT_HOSTS_CONFIGURE_ITEM.
				F("hosts[%d].%s = %v", hc.sequence, key, value)
		}

		v, err := itemset.Build(key, value)
		if err != nil {
			return err
		} else {
			hc.config[key] = v
		}
	}

	privateKeyFile := hc.GetPrivateKeyFile()
	if len(hc.GetName()) == 0 {
		return errno.ERR_HOST_FIELD_MISSING.
			F("hosts[%d].host/name = nil", hc.sequence)
	} else if len(hc.GetHostname()) == 0 {
		return errno.ERR_HOSTNAME_FIELD_MISSING.
			F("hosts[%d].hostname = nil", hc.sequence)
	} else if !utils.IsValidAddress(hc.GetHostname()) {
		return errno.ERR_HOSTNAME_REQUIRES_VALID_IP_ADDRESS.
			F("hosts[%d].hostname = %s", hc.sequence, hc.GetHostname())
	} else if hc.GetSSHPort() > os.GetMaxPortNum() {
		return errno.ERR_HOSTS_SSH_PORT_EXCEED_MAX_PORT_NUMBER.
			F("hosts[%d].ssh_port = %d", hc.sequence, hc.GetSSHPort())
	} else if !strings.HasPrefix(privateKeyFile, "/") {
		return errno.ERR_PRIVATE_KEY_FILE_REQUIRE_ABSOLUTE_PATH.
			F("hosts[%d].private_key_file = %s", hc.sequence, privateKeyFile)
	}

	if !hc.GetForwardAgent() {
		if !utils.PathExist(privateKeyFile) {
			return errno.ERR_PRIVATE_KEY_FILE_NOT_EXIST.
				F("%s: no such file", privateKeyFile)
		} else if utils.GetFilePermissions(privateKeyFile) != PERMISSIONS_600 {
			return errno.ERR_PRIVATE_KEY_FILE_REQUIRE_600_PERMISSIONS.
				F("%s: mode (%d)", privateKeyFile, utils.GetFilePermissions(privateKeyFile))
		}
	}
	return nil
}

// "PORT=112${instancesSquence}" -> "PORT=11201"
func (hc *HostConfig) renderVariables() error {
	//0. get vars
	vars := hc.GetVariables()
	if err := vars.Build(); err != nil {
		log.Error("Build variables failed",
			log.Field("error", err))
		return errno.ERR_RESOLVE_VARIABLE_FAILED.E(err)
	}
	//1. all config to str
	for k, v := range hc.config {
		if v == nil {
			continue
		}
		if strv, ok := utils.All2Str(v); ok {
			hc.config[k] = strv
		} else {
			return errno.ERR_UNSUPPORT_CONFIGURE_VALUE_TYPE.
				F("%s: %v", k, v)
		}
	}
	//2. rendering
	//render labels
	for i := range hc.labels {
		err := func(value *string) error {
			realValue, err := vars.Rendering(*value)
			if err != nil {
				return err
			}
			*value = realValue
			return nil
		}(&hc.labels[i])
		if err != nil {
			return errno.ERR_RENDERING_VARIABLE_FAILED.E(err)
		}
	}
	//render envs
	for i := range hc.envs {
		err := func(value *string) error {
			realValue, err := vars.Rendering(*value)
			if err != nil {
				return err
			}
			*value = realValue
			return nil
		}(&hc.envs[i])
		if err != nil {
			return errno.ERR_RENDERING_VARIABLE_FAILED.E(err)
		}
	}
	//render config
	for k, v := range hc.config {
		if v == nil {
			continue
		}
		realv, err := vars.Rendering(v.(string))
		if err != nil {
			return errno.ERR_RENDERING_VARIABLE_FAILED.E(err)
		}
		hc.config[k] = realv
		build.DEBUG(build.DEBUG_TOPOLOGY,
			build.Field{Key: k, Value: v},
			build.Field{Key: k, Value: realv})
	}
	//3. convert config item to its require type after rendering,
	//	 return error if convert failed
	return hc.convert()
}

func NewHostConfig(sequence int, config map[string]interface{}) *HostConfig {
	vars := variable.NewVariables()

	return &HostConfig{
		sequence:  sequence,
		config:    config,
		labels:    []string{},
		envs:      []string{},
		variables: vars,
		//instances and instancesSquence only used in the memcached deploy
		instances:         1,
		instancesSequence: 1,
	}
}

// deepcopy a HostConfig with instancesSquence and return it (new variables)
func copyHostConfig(src *HostConfig, instancesSquence int) *HostConfig {
	//deepcopy labels
	newlabels := make([]string, len(src.labels))
	copy(newlabels, src.labels)
	//deepcopy envs
	newenvs := make([]string, len(src.envs))
	copy(newenvs, src.envs)
	//create a new variables
	vars := variable.NewVariables()
	return &HostConfig{
		sequence:          src.sequence,
		config:            utils.DeepCopy(src.config),
		labels:            newlabels,
		envs:              newenvs,
		variables:         vars,
		instances:         src.instances,
		instancesSequence: instancesSquence,
	}
}

func ParseHosts(data string) ([]*HostConfig, error) {
	if len(data) == 0 {
		return nil, errno.ERR_EMPTY_HOSTS
	}
	parser := viper.NewWithOptions(viper.KeyDelimiter("::"))
	parser.SetConfigType("yaml")
	err := parser.ReadConfig(bytes.NewBuffer([]byte(data)))
	if err != nil {
		return nil, errno.ERR_PARSE_HOSTS_FAILED.E(err)
	}

	hosts := &Hosts{}
	if err := parser.Unmarshal(hosts); err != nil {
		return nil, errno.ERR_PARSE_HOSTS_FAILED.E(err)
	}

	hcs := []*HostConfig{}
	exist := map[string]bool{}
	for i, host := range hosts.Host {
		host = newIfNil(host)
		merge(hosts.Global, host)
		hc := NewHostConfig(i, host)
		err = hc.Build()
		if err != nil {
			return nil, err
		}

		if _, ok := exist[hc.GetName()]; ok {
			return nil, errno.ERR_DUPLICATE_NAME.
				F("duplicate host: %s", hc.GetName())
		}
		//produce the instances of hc, append to hcs.
		instances := hc.GetInstances()
		for instancesSquence := 1; instancesSquence <= instances; instancesSquence++ {
			hc_new := copyHostConfig(hc, instancesSquence)
			hcs = append(hcs, hc_new)
		}
		exist[hc.GetName()] = true
	}
	//add Variables and Rendering
	for idx, hc := range hcs {
		if err = AddHostVariables(hcs, idx); err != nil {
			return nil, err // already is error code
		} else if err = hc.renderVariables(); err != nil {
			return nil, err // already is error code
		}
		hc.GetVariables().Debug()
	}
	build.DEBUG(build.DEBUG_HOSTS, hosts)
	return hcs, nil
}
