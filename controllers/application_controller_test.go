package controllers

import "testing"

func TestVersionToRFC1123(t *testing.T) {
	got := versionToRFC1123("hello-world", 20)
	if got != "hello-world" {
		t.Errorf("expected \"hello-world\", got %s", got)
	}
	got = versionToRFC1123("hello-world", 10)
	if got != "hello-worl" {
		t.Errorf("expected \"hello-worl\", got %s", got)
	}
	got = versionToRFC1123("hello-world", 6)
	if got != "hello" {
		t.Errorf("expected \"hello\", got %s", got)
	}
	got = versionToRFC1123("hello.1234", 10)
	if got != "hello-1234" {
		t.Errorf("expected \"hello-1234\", got %s", got)
	}
	got = versionToRFC1123("hello-----", 10)
	if got != "hello" {
		t.Errorf("expected \"hello\", got %s", got)
	}
}

func TestEnvVarsToMap(t *testing.T) {
	result := envVarsToMap()
	_, ok := result["PATH"]
	if !ok {
		t.Errorf("Expected environment variables to contain PATH")
	}
}
