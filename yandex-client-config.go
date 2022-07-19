package main

import (
	"errors"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"time"

	"gopkg.in/yaml.v2"
)

type Config struct {
	FolderId        string
	OAuthToken      string
	IamToken        string
	IamTokenExpired time.Time
}

func ReadConfig(file string) (*Config, error) {
	logDebug("read config file %s", file)
	config := new(Config)
	if payload, err := ioutil.ReadFile(file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logDebug("config file not found")
		} else {
			return nil, err
		}
	} else if err := yaml.Unmarshal(payload, config); err != nil {
		return nil, err
	}
	return config, nil
}

func (config *Config) WriteConfig(file string) error {
	logDebug("write config file %s", file)

	if payload, err := yaml.Marshal(config); err != nil {
		return err
	} else if err := os.MkdirAll(path.Dir(file), os.ModePerm); err != nil {
		return err
	} else {
		return ioutil.WriteFile(file, payload, os.ModePerm)
	}
}

func (config *Config) IsIamTokenExpired() bool {
	now := time.Now()
	return len(config.IamToken) == 0 || config.IamTokenExpired.Before(now)
}
