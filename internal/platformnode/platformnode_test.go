package platformnode

import "testing"

func TestIs(t *testing.T) {
	t.Setenv(EnvVar, "")
	if Is("") || Is("abc") {
		t.Error("with no platform node configured, no node is it")
	}
	t.Setenv(EnvVar, " 3ba9d241-e148-47dc-99fc-10f549b2951b ")
	if !Is("3ba9d241-e148-47dc-99fc-10f549b2951b") {
		t.Error("the configured uuid is the platform node")
	}
	if Is("ac409ea1-197c-4609-a46f-e5320864c563") || Is("") {
		t.Error("another node is not")
	}
}
