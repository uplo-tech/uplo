package main

import "strings"

// sanitizeSkylinks will trim away `uplo://` from skylinks
func sanitizeSkylinks(links []string) []string {
	var result []string

	for _, link := range links {
		trimmed := strings.TrimPrefix(link, "uplo://")
		result = append(result, trimmed)
	}

	return result
}
