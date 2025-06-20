package bosh_windows_acceptance_tests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
	"gopkg.in/yaml.v2"
)

func TestBoshWindowsAcceptanceTests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WindowsStemcellAcceptanceTests Suite")
}

type TestConfig struct {
	Bosh struct {
		CaCert       string `json:"ca_cert"`
		Client       string `json:"client"`
		ClientSecret string `json:"client_secret"`
		Target       string `json:"target"`
	} `json:"boshCommand"`
	StemcellPath              string `json:"stemcell_path"`
	StemcellOs                string `json:"stemcell_os"`
	Az                        string `json:"az"`
	VmType                    string `json:"vm_type"`
	RootEphemeralVmType       string `json:"root_ephemeral_vm_type"`
	VmExtensions              string `json:"vm_extensions"`
	Network                   string `json:"network"`
	DefaultUsername           string `json:"default_username"`
	DefaultPassword           string `json:"default_password"`
	SkipCleanup               bool   `json:"skip_cleanup"`
	MountEphemeralDisk        bool   `json:"mount_ephemeral_disk"`
	SkipMSUpdateTest          bool   `json:"skip_ms_update_test"`
	SSHDisabledByDefault      bool   `json:"ssh_disabled_by_default"`
	SecurityComplianceApplied bool   `json:"security_compliance_applied"`
}

var _ = BeforeSuite(func() {
	configFilePath := os.Getenv("CONFIG_JSON")
	Expect(configFilePath).ToNot(BeEmpty(), fmt.Sprintf("invalid testConfig file path: %v", configFilePath))

	body, err := os.ReadFile(configFilePath)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("empty testConfig file path: %v", configFilePath))

	err = json.Unmarshal(body, &testConfig)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("unable to parse testConfig file: %s: %s", err.Error(), string(body)))

	Expect(testConfig.StemcellOs).ToNot(BeEmpty(), fmt.Sprintf("missing required field: %v", "stemcell_os"))

	if testConfig.VmExtensions == "" {
		testConfig.VmExtensions = "500GB_ephemeral_disk"
	}

	boshCommand = newBoshCommand(testConfig)

	err = boshCommand.Run("login")
	Expect(err).NotTo(HaveOccurred())
	deploymentName = fmt.Sprintf("windows-acceptance-test-%d", getTimestampInMs())

	stemcellYML, err := fetchStemcellInfo(testConfig.StemcellPath)
	Expect(err).NotTo(HaveOccurred())

	stemcellName = stemcellYML.Name
	stemcellVersion = stemcellYML.Version

	releaseVersion = createBwatsRelease(boshCommand)

	uploadStemcell(testConfig, boshCommand)

	err = testConfig.deploy(boshCommand, deploymentName, stemcellVersion, releaseVersion)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	// Delete the releases created by the tight loop test
	for index, version := range tightLoopStemcellVersions {
		if index == len(tightLoopStemcellVersions)-1 {
			continue // Last release is still being used by the deployment, so it cannot be deleted yet
		}
		err := boshCommand.Run(fmt.Sprintf("delete-release bwats-release/%s", version))
		Expect(err).NotTo(HaveOccurred())
	}
	if testConfig.SkipCleanup {
		return
	}

	err := boshCommand.Run(fmt.Sprintf("-d %s delete-deployment --force", deploymentName))
	Expect(err).NotTo(HaveOccurred())
	err = boshCommand.Run(fmt.Sprintf("delete-stemcell %s/%s", stemcellName, stemcellVersion))
	Expect(err).NotTo(HaveOccurred())
	err = boshCommand.Run(fmt.Sprintf("delete-release bwats-release/%s", releaseVersion))
	Expect(err).NotTo(HaveOccurred())
	if len(tightLoopStemcellVersions) != 0 {
		err = boshCommand.Run(fmt.Sprintf("delete-release bwats-release/%s", tightLoopStemcellVersions[len(tightLoopStemcellVersions)-1]))
		Expect(err).NotTo(HaveOccurred())
	}

	if boshCommand.CertPath != "" {
		Expect(os.RemoveAll(boshCommand.CertPath)).To(Succeed())
	}
})

func getTimestampInMs() int64 {
	return time.Now().UTC().UnixNano() / int64(time.Millisecond)
}

type StemcellYML struct {
	Version string `yaml:"version"`
	Name    string `yaml:"name"`
}

func fetchStemcellInfo(stemcellPath string) (StemcellYML, error) {
	var stemcellInfo StemcellYML
	tempDir, err := os.MkdirTemp("", "")
	Expect(err).NotTo(HaveOccurred())
	defer os.RemoveAll(tempDir) //nolint:errcheck

	cmd := exec.Command("tar", "xf", stemcellPath, "-C", tempDir, "stemcell.MF")
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	Eventually(session, 20*time.Minute).Should(gexec.Exit())

	exitCode := session.ExitCode()
	if exitCode != 0 {
		var stderr []byte
		if session.Err != nil {
			stderr = session.Err.Contents()
		}
		stdout := session.Out.Contents()
		return stemcellInfo, fmt.Errorf("Non-zero exit code for cmd %q: %d\nSTDERR:\n%s\nSTDOUT:%s\n",
			strings.Join(cmd.Args, " "), exitCode, stderr, stdout)
	}

	stemcellMF, err := os.ReadFile(fmt.Sprintf("%s/%s", tempDir, "stemcell.MF"))
	Expect(err).NotTo(HaveOccurred())

	err = yaml.Unmarshal(stemcellMF, &stemcellInfo)
	Expect(err).NotTo(HaveOccurred())
	Expect(stemcellInfo.Version).ToNot(BeNil())
	Expect(stemcellInfo.Version).ToNot(BeEmpty())

	return stemcellInfo, nil
}

type BoshCommand struct {
	DirectorIP   string
	Client       string
	ClientSecret string
	CertPath     string // Path to CA CERT file, if any
	Timeout      time.Duration
}

func newBoshCommand(config *TestConfig) *BoshCommand {
	var boshCertPath string
	cert := config.Bosh.CaCert
	if cert != "" {
		certFile, err := os.CreateTemp("", "")
		Expect(err).NotTo(HaveOccurred())

		_, err = certFile.Write([]byte(cert))
		Expect(err).NotTo(HaveOccurred())

		boshCertPath, err = filepath.Abs(certFile.Name())
		Expect(err).NotTo(HaveOccurred())
	}

	timeout := BoshTimeout
	var err error
	if s := os.Getenv("BWATS_BOSH_TIMEOUT"); s != "" {
		timeout, err = time.ParseDuration(s)
		GinkgoWriter.Printf("Using BWATS_BOSH_TIMEOUT (%s) as timeout\n", s)

		if err != nil {
			GinkgoWriter.Printf("Error parsing BWATS_BOSH_TIMEOUT (%s): %s - falling back to default\n", s, err)
		}
	}

	return &BoshCommand{
		DirectorIP:   config.Bosh.Target,
		Client:       config.Bosh.Client,
		ClientSecret: config.Bosh.ClientSecret,
		CertPath:     boshCertPath,
		Timeout:      timeout,
	}
}

func (c *BoshCommand) args(command string) []string {
	args := strings.Split(command, " ")
	args = append([]string{"-n", "-e", c.DirectorIP, "--client", c.Client, "--client-secret", c.ClientSecret}, args...)
	if c.CertPath != "" {
		args = append([]string{"--ca-cert", c.CertPath}, args...)
	}
	return args
}

func (c *BoshCommand) Run(command string) error {
	return c.RunIn(command, "")
}

func (c *BoshCommand) RunErrand(errandName string, deploymentName string) error {
	return c.Run(fmt.Sprintf("-d %s run-errand --download-logs %s --tty", deploymentName, errandName))
}

func (c *BoshCommand) RunInStdOut(command, dir string) ([]byte, error) {
	cmd := exec.Command("bosh", c.args(command)...)

	if dir != "" {
		cmd.Dir = dir
		GinkgoWriter.Printf("\nRUNNING %q IN %q\n", strings.Join(cmd.Args, " "), dir)
	} else {
		GinkgoWriter.Printf("\nRUNNING %q\n", strings.Join(cmd.Args, " "))
	}

	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	if err != nil {
		return nil, err
	}
	Eventually(session, c.Timeout).Should(gexec.Exit())

	exitCode := session.ExitCode()
	stdout := session.Out.Contents()
	if exitCode != 0 {
		var stderr []byte
		if session.Err != nil {
			stderr = session.Err.Contents()
		}
		return stdout,
			fmt.Errorf(
				"Non-zero exit code for cmd %q: %d\nSTDERR:\n%s\nSTDOUT:%s\n",
				strings.Join(cmd.Args, " "), exitCode, stderr, stdout,
			)
	}
	return stdout, nil
}

func (c *BoshCommand) RunIn(command, dir string) error {
	_, err := c.RunInStdOut(command, dir)
	return err
}
