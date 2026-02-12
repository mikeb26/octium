/* Copyright © 2026 Mike Brown. All Rights Reserved.
 *
 * See LICENSE file at the root of this package for license terms
 */
package httpproxy

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/mikeb26/octium/internal/am"
)

func (httpProxy *HttpProxy) isRequestAllowedByPolicy(r *http.Request) (bool, error) {
	policyID, requiredActions, ok := buildPolicyIDForRequest(r)
	if !ok {
		return false, nil
	}

	actions, found := httpProxy.policyStore.Check(policyID)
	if !found {
		return false, nil
	}
	if !hasAllApprovalActions(actions, requiredActions) {
		return false, nil
	}

	return true, nil
}

func buildPolicyIDForRequest(r *http.Request) (string, []am.ApprovalAction, bool) {
	host, port, scheme, ok := requestTarget(r)
	if !ok {
		return "", nil, false
	}

	domainKey := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port))
	policyID := am.ApprovalPolicyID(am.ApprovalSubsysTools, am.ApprovalGroupWeb, am.ApprovalTargetDomain, domainKey)

	method := strings.ToUpper(strings.TrimSpace(r.Method))
	if method == "" {
		method = "GET"
	}

	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return policyID, []am.ApprovalAction{am.ApprovalActionRead}, true
	default:
		return policyID, []am.ApprovalAction{am.ApprovalActionWrite, am.ApprovalActionRead}, true
	}
}

func requestTarget(r *http.Request) (host, port, scheme string, ok bool) {
	if r.Method == http.MethodConnect {
		scheme = "https"
		target := strings.TrimSpace(r.Host)
		if target == "" {
			return "", "", "", false
		}

		// If Host lacks a port, default to 443.
		if !strings.Contains(target, ":") {
			target += ":443"
		}

		h, p, err := net.SplitHostPort(target)
		if err != nil {
			return "", "", "", false
		}
		if h == "" || p == "" {
			return "", "", "", false
		}
		return h, p, scheme, true
	}

	// For an HTTP forward proxy, clients typically send absolute-form URLs.
	// If we get origin-form, reconstruct a URL from Host + RequestURI.
	outURL := *r.URL
	if outURL.Scheme == "" {
		outURL.Scheme = "http"
	}
	if outURL.Host == "" {
		outURL.Host = r.Host
	}
	if outURL.Scheme == "" || outURL.Host == "" {
		return "", "", "", false
	}

	scheme = strings.ToLower(outURL.Scheme)
	host = outURL.Hostname()
	if host == "" {
		return "", "", "", false
	}
	port = outURL.Port()
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		default:
			return "", "", "", false
		}
	}

	return host, port, scheme, true
}

func hasAllApprovalActions(have, need []am.ApprovalAction) bool {
	if len(need) == 0 {
		return true
	}

	set := make(map[am.ApprovalAction]struct{}, len(have))
	for _, a := range have {
		set[a] = struct{}{}
	}
	for _, a := range need {
		if _, ok := set[a]; !ok {
			return false
		}
	}
	return true
}
