package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Helper function to accept and format both the project ID or name as project
// identifier for all API calls.
// Stolen from https://github.com/xanzy/go-gitlab
func parseID(id interface{}) (string, error) {
	switch v := id.(type) {
	case int:
		return strconv.Itoa(v), nil
	case string:
		return v, nil
	default:
		return "", fmt.Errorf("invalid ID type %#v, the ID must be an int or a string", id)
	}
}

// Helper function to escape a project identifier.
// Stolen from https://github.com/xanzy/go-gitlab
func pathEscape(s string) string {
	return strings.Replace(url.PathEscape(s), ".", "%2E", -1)
}

func ValidateEnv(env string) (string, error) {

	value, ok := os.LookupEnv(env)
	if !ok {
		return value, fmt.Errorf("var %s is not set! value: %q", env, value)
	}
	if value == "" {
		return value, fmt.Errorf("var %s is empty! value: %q", env, value)
	}

	return value, nil
}
