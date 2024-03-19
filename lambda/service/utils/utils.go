package utils

import "regexp"

func ExtractRoute(requestRouteKey string) string {
	r := regexp.MustCompile(`(?P<method>) (?P<pathKey>.*)`)
	routeKeyParts := r.FindStringSubmatch(requestRouteKey)
	return routeKeyParts[r.SubexpIndex("pathKey")]
}

func ExtractParam(rawPath string) string {
	r := regexp.MustCompile(`/pennsieve-accounts/(?P<accountType>.*)`)
	tokenParts := r.FindStringSubmatch(rawPath)
	return tokenParts[r.SubexpIndex("accountType")]
}
