package util

import (
	"os"
	"path/filepath"
)

// GetCacheDir returns the caching directory for module
func GetCacheDir(repo, chart string) (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	targetDir := filepath.Join(userCacheDir, repo, chart)
	return targetDir, nil
}

// GetConfigDir returns the config directory for k8ssandra and creates it if it does not exists
func GetConfigDir(repo, chart string) (string, error) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}

	targetDir := filepath.Join(userConfigDir, repo, chart)
	return CreateIfNotExistsDir(targetDir)
}

func CreateIfNotExistsDir(targetDir string) (string, error) {
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", err
		}
	}
	return targetDir, nil
}

func VerifyFileExists(yamlPath string) (bool, error) {
	if _, err := os.Stat(yamlPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
