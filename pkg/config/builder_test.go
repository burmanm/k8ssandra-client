package config

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/k8ssandra/k8ssandra-client/internal/envtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var existingConfig = `
{
	"cassandra-env-sh": {
		"malloc-arena-max": 8,
		"additional-jvm-opts": [
		"-Dcassandra.system_distributed_replication=test-dc:1",
		"-Dcom.sun.management.jmxremote.authenticate=true"
		]
	},
	"jvm-server-options": {
		"initial_heap_size": "512m",
		"max_heap_size": "512m",
		"per_thread_stack_size": "384k",
		"additional-jvm-opts": [
		"-Dcassandra.system_distributed_replication=test-dc:1",
		"-Dcom.sun.management.jmxremote.authenticate=true"
		]
	},
	"jvm11-server-options": {
		"g1r_set_updating_pause_time_percent": "6",
		"additional-jvm-opts": [
		"-XX:MaxGCPauseMillis=350"
		]
	},
	"cassandra-yaml": {
		"authenticator": "PasswordAuthenticator",
		"authorizer": "CassandraAuthorizer",
		"num_tokens": 256,
		"role_manager": "CassandraRoleManager",
		"start_rpc": false
	},
	"cluster-info": {
		"name": "test",
		"seeds": "test-seed-service,test-dc-additional-seed-service"
	},
	"datacenter-info": {
		"graph-enabled": 0,
		"name": "datacenter1",
		"solr-enabled": 0,
		"spark-enabled": 0
	}
}
`

var numericConfig = `
{
	"jvm-server-options": {
		"max_heap_size": 524288000
	},
	"cassandra-yaml": {
		"authenticator": "PasswordAuthenticator",
		"authorizer": "CassandraAuthorizer",
		"num_tokens": 256,
		"role_manager": "CassandraRoleManager",
		"start_rpc": false
	},
	"cluster-info": {
		"name": "test",
		"seeds": "test-seed-service,test-dc-additional-seed-service"
	},
	"datacenter-info": {
		"graph-enabled": 0,
		"name": "dc2",
		"solr-enabled": 0,
		"spark-enabled": 0
	}
}
`

func TestBuilderDefaults(t *testing.T) {
	require := require.New(t)
	builder := NewBuilder("", "")
	require.Equal(defaultInputDir, builder.configInputDir)
	require.Equal(defaultOutputDir, builder.configOutputDir)
}

func TestConfigInfoParsing(t *testing.T) {
	require := require.New(t)
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)
	require.NotNil(configInput.CassYaml)
	require.NotNil(configInput.ClusterInfo)
	require.NotNil(configInput.DatacenterInfo)

	require.Equal("test", configInput.ClusterInfo.Name)
	require.Equal("datacenter1", configInput.DatacenterInfo.Name)
}

func TestParseNodeInfo(t *testing.T) {
	require := require.New(t)
	t.Setenv("POD_IP", "172.27.0.1")
	t.Setenv("RACK_NAME", "r1")
	nodeInfo, err := parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)
	require.Equal("172.27.0.1", nodeInfo.ListenIP.String())
	require.Equal("172.27.0.1", nodeInfo.BroadcastIP.String())
	require.Equal("0.0.0.0", nodeInfo.RPCIP.String())
	require.Equal("r1", nodeInfo.Rack)

	t.Setenv("HOST_IP", "10.0.0.1")
	nodeInfo, err = parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)
	require.Equal("172.27.0.1", nodeInfo.ListenIP.String())
	require.Equal("172.27.0.1", nodeInfo.BroadcastIP.String())

	t.Setenv("USE_HOST_IP_FOR_BROADCAST", "false")
	nodeInfo, err = parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)
	require.Equal("172.27.0.1", nodeInfo.ListenIP.String())
	require.Equal("172.27.0.1", nodeInfo.BroadcastIP.String())

	t.Setenv("USE_HOST_IP_FOR_BROADCAST", "true")
	nodeInfo, err = parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)
	require.Equal("172.27.0.1", nodeInfo.ListenIP.String())
	require.Equal("10.0.0.1", nodeInfo.BroadcastIP.String())
}

func TestBuild(t *testing.T) {
	require := require.New(t)
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	inputDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")
	require.NoError(err)
	defer os.RemoveAll(tempDir)

	b := NewBuilder(inputDir, tempDir)
	require.NoError(b.Build(context.TODO()))

	// Verify that all target files are there..
	entries, err := os.ReadDir(tempDir)
	require.NoError(err)

	fileNames := make([]string, 0, len(entries))
	for _, v := range entries {
		fileNames = append(fileNames, v.Name())
		f, err := v.Info()
		require.NoError(err)
		require.True(f.Size() > 0)
	}

	require.Contains(fileNames, "cassandra-env.sh")
	require.Contains(fileNames, "cassandra-rackdc.properties")
	require.Contains(fileNames, "cassandra.yaml")
	require.Contains(fileNames, "jvm-server.options")
	require.Contains(fileNames, "jvm11-server.options")
}

func TestCassandraYamlWriting(t *testing.T) {
	require := require.New(t)
	cassYamlDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")
	require.NoError(err)

	defer os.RemoveAll(tempDir)

	// Create mandatory configs..
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)
	t.Setenv("POD_IP", "172.27.0.1")
	t.Setenv("RACK_NAME", "r1")
	nodeInfo, err := parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)

	require.NoError(createCassandraYaml(configInput, nodeInfo, cassYamlDir, tempDir))

	yamlOrigPath := filepath.Join(cassYamlDir, "cassandra.yaml")
	yamlPath := filepath.Join(tempDir, "cassandra.yaml")

	yamlOrigFile, err := os.ReadFile(yamlOrigPath)
	require.NoError(err)

	yamlFile, err := os.ReadFile(yamlPath)
	require.NoError(err)

	// Unmarshal, Marshal to remove all comments (and some fields if necessary)
	cassandraYaml := make(map[string]interface{})
	require.NoError(yaml.Unmarshal(yamlFile, cassandraYaml))

	cassandraOrigYaml := make(map[string]interface{})
	require.NoError(yaml.Unmarshal(yamlOrigFile, cassandraOrigYaml))

	// Verify all the original keys are there (nothing was removed)
	for k := range cassandraOrigYaml {
		require.Contains(cassandraYaml, k)
	}

	// Verify our k8ssandra overrides are set
	clusterName := configInput.ClusterInfo.Name
	require.Equal(clusterName, cassandraYaml["cluster_name"])

	seedProviders := cassandraYaml["seed_provider"].([]interface{})
	seedProvider := seedProviders[0].(map[string]interface{})
	require.Equal("org.apache.cassandra.locator.K8SeedProvider", seedProvider["class_name"])
	require.Equal("GossipingPropertyFileSnitch", cassandraYaml["endpoint_snitch"])

	listenIP := nodeInfo.ListenIP.String()
	require.Equal(listenIP, cassandraYaml["listen_address"])

	// Verify our changed properties are there
	require.Equal("PasswordAuthenticator", cassandraYaml["authenticator"])
	require.Equal("CassandraAuthorizer", cassandraYaml["authorizer"])
	require.Equal("CassandraRoleManager", cassandraYaml["role_manager"])
	require.Equal("256", cassandraYaml["num_tokens"])
	require.Equal(false, cassandraYaml["start_rpc"])
}

func TestRackProperties(t *testing.T) {
	require := require.New(t)
	propertiesDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")
	defer os.RemoveAll(tempDir)
	require.NoError(err)

	// Create mandatory configs..
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)
	t.Setenv("POD_IP", "172.27.0.1")
	t.Setenv("RACK_NAME", "r1")
	nodeInfo, err := parseNodeInfo()
	require.NoError(err)
	require.NotNil(nodeInfo)

	require.NoError(createRackProperties(configInput, nodeInfo, propertiesDir, tempDir))

	lines, err := readFileToLines(tempDir, "cassandra-rackdc.properties")
	require.NoError(err)
	require.Equal(2, len(lines))
	require.Contains(lines, "dc=datacenter1")
	require.Contains(lines, "rack=r1")
}

func TestServerOptionsOutput(t *testing.T) {
	require := require.New(t)
	optionsDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")

	defer os.RemoveAll(tempDir)
	require.NoError(err)

	// Create mandatory configs..
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)

	require.NoError(createJVMOptions(configInput, optionsDir, tempDir))

	inputFile := filepath.Join(tempDir, "jvm-server.options")
	inputFile11 := filepath.Join(tempDir, "jvm11-server.options")

	s, err := readJvmServerOptions(inputFile)
	require.NoError(err)

	require.Contains(s, "-Xss384k")
	require.NotContains(s, "-Xss256k")

	require.Contains(s, "-Xmx512m")
	require.Contains(s, "-Xms512m")
	require.Contains(s, "-Dcassandra.system_distributed_replication=test-dc:1")
	require.Contains(s, "-Dcom.sun.management.jmxremote.authenticate=true")

	s11, err := readJvmServerOptions(inputFile11)
	require.NoError(err)

	require.Contains(s11, "-XX:MaxGCPauseMillis=350")
	require.NotContains(s11, "-XX:MaxGCPauseMillis=300")
	require.Contains(s11, "-XX:G1RSetUpdatingPauseTimePercent=6")
	require.NotContains(s11, "-XX:G1RSetUpdatingPauseTimePercent=5")

	for _, v := range defaultG1Settings {
		if v == "-XX:G1RSetUpdatingPauseTimePercent=5" || v == "-XX:MaxGCPauseMillis=300" {
			// Our config replaces these default values with new values, so they should not be here
			require.NotContains(s11, v)
			continue
		}
		require.Contains(s11, v)
	}

	// Test empty also and check we get the default G1 settings
	ci := &ConfigInput{}
	tempDir2, err := os.MkdirTemp("", "client-test")
	require.NoError(err)
	defer os.RemoveAll(tempDir2)
	require.NoError(createJVMOptions(ci, optionsDir, tempDir2))

	inputFile11 = filepath.Join(tempDir2, "jvm11-server.options")

	s11, err = readJvmServerOptions(inputFile11)
	require.NoError(err)

	for _, v := range defaultG1Settings {
		require.Contains(s11, v)
	}

	// Test CMS option also
	ci = &ConfigInput{
		ServerOptions11: map[string]interface{}{
			"garbage_collector": "CMS",
		},
	}

	tempDir3, err := os.MkdirTemp("", "client-test")
	require.NoError(err)
	defer os.RemoveAll(tempDir3)
	require.NoError(createJVMOptions(ci, optionsDir, tempDir3))

	inputFile11 = filepath.Join(tempDir3, "jvm11-server.options")

	s11, err = readJvmServerOptions(inputFile11)
	require.NoError(err)

	for _, v := range defaultCMSSettings {
		require.Contains(s11, v)
	}
}

func TestGCOptions(t *testing.T) {
	assert := assert.New(t)
	assert.Equal(defaultG1Settings, getGCOptions("G1GC", 11))
	assert.Equal(defaultG1Settings, getGCOptions("G1GC", 17))

	assert.Equal(defaultCMSSettings, getGCOptions("CMS", 11))
	assert.Equal(defaultCMSSettings, getGCOptions("CMS", 17))

	assert.Equal([]string{"-XX:+UseShenandoahGC"}, getGCOptions("Shenandoah", 11))
	assert.Equal([]string{"-XX:+UseShenandoahGC"}, getGCOptions("Shenandoah", 17))

	assert.Equal([]string{"-XX:+UseZGC", "-XX:+UnlockExperimentalVMOptions"}, getGCOptions("ZGC", 11))
	assert.Equal([]string{"-XX:+UseZGC"}, getGCOptions("ZGC", 17))
}

func TestCassandraEnv(t *testing.T) {
	require := require.New(t)
	envDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")
	defer os.RemoveAll(tempDir)

	require.NoError(err)

	// Create mandatory configs..
	t.Setenv("CONFIG_FILE_DATA", existingConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)

	require.NoError(createCassandraEnv(configInput, envDir, tempDir))

	// Verify output
	lines, err := readFileToLines(tempDir, "cassandra-env.sh")
	require.NoError(err)

	require.Contains(lines, "export MALLOC_ARENA_MAX=8")
	require.Contains(lines, "JVM_OPTS=\"$JVM_OPTS -Dcassandra.system_distributed_replication=test-dc:1\"")
	require.Contains(lines, "JVM_OPTS=\"$JVM_OPTS -Dcom.sun.management.jmxremote.authenticate=true\"")
}

func TestReadOptionsWithNumeric(t *testing.T) {
	// JSON Unmarshalling does not Unmarshal everything to type string, instead they can be int/floats/bool etc
	require := require.New(t)

	optionsDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")

	defer os.RemoveAll(tempDir)
	require.NoError(err)

	t.Setenv("CONFIG_FILE_DATA", numericConfig)
	configInput, err := parseConfigInput()
	require.NoError(err)
	require.NotNil(configInput)

	require.NoError(createJVMOptions(configInput, optionsDir, tempDir))

	lines, err := readFileToLines(tempDir, "jvm-server.options")
	require.NoError(err)

	require.Contains(lines, "-Xmx524288000")
}

// readFileToLines is a small test helper, reads file to []string (per line). This version does not filter anything, not even whitespace.
func readFileToLines(dir, filename string) ([]string, error) {
	outputFile := filepath.Join(dir, filename)
	lines := make([]string, 0, 1)

	f, err := os.Open(outputFile)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func TestCopyFiles(t *testing.T) {
	require := require.New(t)
	inputDir := filepath.Join(envtest.RootDir(), "testfiles")
	tempDir, err := os.MkdirTemp("", "client-test")
	require.NoError(err)
	defer os.RemoveAll(tempDir)

	require.NoError(copyFiles(inputDir, tempDir))

	// We should have tempDir/jvm11-clients.options
	_, err = os.Stat(filepath.Join(tempDir, "jvm11-clients.options"))
	require.NoError(err)
}
