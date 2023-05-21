package main

import (
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"time"

	"gopkg.in/yaml.v2"
)

type Config struct {
	FolderID       string
	OAuthToken     string
	IamToken       string
	IamTokenExpire time.Time
}

func ReadConfig(file string) (*Config, error) {
	logDebugf("read config file %s", file)
	config := new(Config)
	if payload, err := ioutil.ReadFile(file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logDebugf("config file not found")
		} else {
			return nil, err
		}
	} else if err := yaml.Unmarshal(payload, config); err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}
	return config, nil
}

func WriteConfig(config *Config, file string) error {
	logDebugf("write config file %s", file)
	if payload, err := yaml.Marshal(config); err != nil {
		return err
	} else if err := os.MkdirAll(path.Dir(file), os.ModePerm); err != nil {
		return err
	} else {
		return ioutil.WriteFile(file, payload, os.ModePerm)
	}
}

func (config *Config) Store(file string) {
	if len(file) == 0 {
		return
	} else if err := WriteConfig(config, file); err != nil {
		logError(fmt.Errorf("wirte config file: %w", err))
	}
}

func (config *Config) IsIamTokenExpired() bool {
	now := time.Now()
	return len(config.IamToken) == 0 || config.IamTokenExpire.Before(now)
}

func (config *Config) UpdateIamToken(token string, expire time.Time) {
	config.IamToken = token
	config.IamTokenExpire = expire
}
