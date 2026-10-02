package ansible

import (
	"strconv"
	"strings"
	"testing"

	"github.com/28Pollux28/galvanize/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePublishedPorts_ExtractsOptionalProtocolHints(t *testing.T) {
	params := map[string]interface{}{
		"image": "example:latest",
		"published_ports": []interface{}{
			"22/ssh",
			"8080:80/http",
			"53/udp",
			443,
		},
	}

	normalized, hints, bindings := normalizePublishedPortsWithState(params, false, nil, false, defaultPortRange)

	assert.Equal(t, "example:latest", normalized["image"])
	assert.Empty(t, bindings)

	ports, ok := normalized["published_ports"].([]interface{})
	require.True(t, ok)
	assert.Equal(t, []interface{}{"22", "8080:80", "53/udp", 443}, ports)

	assert.Equal(t, "ssh", hints[22])
	assert.Equal(t, "http", hints[80])
	_, hasUDPHint := hints[53]
	assert.False(t, hasUDPHint)
	_, has443Hint := hints[443]
	assert.False(t, has443Hint)
}

func TestGetConnectionInfo_UsesHintedProtocolWhenProvided(t *testing.T) {
	containers := []ContainerInfo{
		{
			Publishers: []PublisherInfo{
				{Protocol: "tcp", PublishedPort: 40022, TargetPort: 22, URL: "0.0.0.0"},
				{Protocol: "tcp", PublishedPort: 40080, TargetPort: 80, URL: "0.0.0.0"},
			},
		},
	}

	conn, err := GetConnectionInfo(containers, "instancer.example.com", map[int]string{22: "ssh"})
	require.NoError(t, err)
	assert.Equal(t, "ssh://instancer.example.com:40022\ntcp://instancer.example.com:40080", conn)
}

func TestGetConnectionInfo_UsesDockerProtocolWhenNoHint(t *testing.T) {
	containers := []ContainerInfo{
		{
			Publishers: []PublisherInfo{
				{Protocol: "tcp", PublishedPort: 40022, TargetPort: 22, URL: "0.0.0.0"},
			},
		},
	}

	conn, err := GetConnectionInfo(containers, "instancer.example.com", nil)
	require.NoError(t, err)
	assert.Equal(t, "tcp://instancer.example.com:40022", conn)
}

func TestNormalizePublishedPorts_RandomizeHostPorts_WhenEnabled(t *testing.T) {
	params := map[string]interface{}{
		"published_ports": []interface{}{
			"22/ssh",
			"53/udp",
			"8080:80/http",
			443,
		},
	}

	normalized, hints, bindings := normalizePublishedPortsWithState(params, true, nil, true, defaultPortRange)
	ports, ok := normalized["published_ports"].([]interface{})
	require.True(t, ok)
	require.Len(t, ports, 4)

	first, ok := ports[0].(string)
	require.True(t, ok)
	assert.True(t, isRandomBindingFor(first, "22"))

	second, ok := ports[1].(string)
	require.True(t, ok)
	assert.True(t, isRandomBindingFor(second, "53/udp"))

	third, ok := ports[2].(string)
	require.True(t, ok)
	assert.Equal(t, "8080:80", third)

	fourth, ok := ports[3].(string)
	require.True(t, ok)
	assert.True(t, isRandomBindingFor(fourth, "443"))

	assert.Equal(t, "ssh", hints[22])
	assert.Equal(t, "http", hints[80])
	assert.NotEmpty(t, bindings)
}

var defaultPortRange = portRange{lo: config.DefaultRandomizedPortMin, hi: config.DefaultRandomizedPortMax}

func isRandomBindingFor(portDef string, target string) bool {
	parts := strings.Split(portDef, ":")
	if len(parts) != 2 {
		return false
	}
	if parts[1] != target {
		return false
	}
	hostPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	return defaultPortRange.contains(hostPort)
}

func webContainer(service, project, domain string) ContainerInfo {
	return ContainerInfo{
		Name: project + "-" + service + "-1",
		Labels: map[string]string{
			"com.docker.compose.service":                                     service,
			"traefik.enable":                                                 "true",
			"traefik.http.routers." + project + ".rule":                      "Host(`" + domain + "`)",
			"traefik.http.services." + project + ".loadbalancer.server.port": "80",
		},
	}
}

func sshContainer(project string, port int) ContainerInfo {
	return ContainerInfo{
		Name:   project + "-ssh-1",
		Labels: map[string]string{"com.docker.compose.service": "ssh"},
		Publishers: []PublisherInfo{
			{Protocol: "tcp", PublishedPort: port, TargetPort: 22, URL: "0.0.0.0"},
			{Protocol: "tcp", PublishedPort: port, TargetPort: 22, URL: "::"},
		},
	}
}

func TestGetConnectionInfo_SingleWebChallengeUnchanged(t *testing.T) {
	conn, err := GetConnectionInfo([]ContainerInfo{webContainer("web", "p", "p.challs.example.com")}, "challs.example.com", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://p.challs.example.com/", conn)
}

// A web front end and an SSH service: both endpoints, whatever order Ansible
// reports the containers in. Only the first container's endpoint was returned.
func TestGetConnectionInfo_WebAndSSHServices(t *testing.T) {
	web := webContainer("web", "p", "p.challs.example.com")
	ssh := sshContainer("p", 40022)
	want := "tcp://challs.example.com:40022\nhttps://p.challs.example.com/"
	hints := map[int]string{}

	for _, order := range [][]ContainerInfo{{web, ssh}, {ssh, web}} {
		conn, err := GetConnectionInfo(order, "challs.example.com", hints)
		require.NoError(t, err)
		assert.Equal(t, want, conn, "ssh sorts before web")
	}

	hints[22] = "ssh"
	conn, err := GetConnectionInfo([]ContainerInfo{web, ssh}, "challs.example.com", hints)
	require.NoError(t, err)
	assert.Equal(t, "ssh://challs.example.com:40022\nhttps://p.challs.example.com/", conn)
}

func TestGetConnectionInfo_SeveralWebServices(t *testing.T) {
	conn, err := GetConnectionInfo([]ContainerInfo{
		webContainer("web", "web-p", "web-p.challs.example.com"),
		webContainer("api", "api-p", "api-p.challs.example.com"),
	}, "challs.example.com", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://api-p.challs.example.com/\nhttps://web-p.challs.example.com/", conn)
}

// Router labels other than rules (an author's own entrypoints, tls,
// middlewares) were picked at random among the router labels
func TestGetConnectionInfo_OnlyRouterRules(t *testing.T) {
	web := webContainer("web", "p", "p.challs.example.com")
	web.Labels["traefik.http.routers.p.entrypoints"] = "websecure"
	web.Labels["traefik.http.routers.p.tls"] = "true"
	web.Labels["traefik.http.routers.p.middlewares"] = "auth"

	for range 20 {
		conn, err := GetConnectionInfo([]ContainerInfo{web}, "challs.example.com", nil)
		require.NoError(t, err)
		require.Equal(t, "https://p.challs.example.com/", conn)
	}
}

func TestGetConnectionInfo_RuleWithSeveralHosts(t *testing.T) {
	web := webContainer("web", "p", "")
	web.Labels["traefik.http.routers.p.rule"] = "Host(`a.challs.example.com`) || (Host(`b.challs.example.com`) && PathPrefix(`/x`))"
	conn, err := GetConnectionInfo([]ContainerInfo{web}, "challs.example.com", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://a.challs.example.com/\nhttps://b.challs.example.com/", conn)
}

func TestGetConnectionInfo_TraefikDisabledIgnored(t *testing.T) {
	web := webContainer("web", "p", "p.challs.example.com")
	web.Labels["traefik.enable"] = "false"
	conn, err := GetConnectionInfo([]ContainerInfo{web, sshContainer("p", 40022)}, "challs.example.com", map[int]string{22: "ssh"})
	require.NoError(t, err)
	assert.Equal(t, "ssh://challs.example.com:40022", conn)
}

func TestGetConnectionInfo_NoEndpoint(t *testing.T) {
	_, err := GetConnectionInfo([]ContainerInfo{{Name: "db", Labels: map[string]string{"com.docker.compose.service": "db"}}}, "challs.example.com", nil)
	assert.Error(t, err)
}
