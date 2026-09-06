//go:build windows

package main

import (
	"golang.org/x/sys/windows/registry"
)

func registryOpenKey(path string) (registry.Key, error) {
	return registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
}

func registryOpenHKLM(path string) (registry.Key, error) {
	return registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
}
