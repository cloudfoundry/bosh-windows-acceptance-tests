package bosh_windows_acceptance_tests_test

import (
	"time"
	"os"
	"fmt"
	"io/ioutil"
	"encoding/json"
	"path/filepath"
	"log"
	"strings"
	"os/exec"
	"io"
	"errors"
	"bytes"
	"gopkg.in/yaml.v2"
	"net/http"

	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const BOSH_TIMEOUT = 90 * time.Minute

const GoZipFile = "go1.7.1.windows-amd64.zip"
const GolangURL = "https://storage.googleapis.com/golang/" + GoZipFile

// If this URL becomes invalid then we will need to configure an external blobstore for bwats-release
const MbsaFile = "MBSASetup-x64-EN.msi"
const MbsaURL = "https://download.microsoft.com/download/8/E/1/8E16A4C7-DD28-4368-A83A-282C82FC212A/MBSASetup-x64-EN.msi"

const redeployRetries = 10

type ManifestProperties struct {
	DeploymentName     string
	ReleaseName        string
	AZ                 string
	VmType             string
	VmExtensions       string
	Network            string
	StemcellOs         string
	StemcellVersion    string
	ReleaseVersion     string
	MountEphemeralDisk bool
}

type StemcellYML struct {
	Version string `yaml:"version"`
	Name    string `yaml:"name"`
}

type Config struct {
	Bosh struct {
		CaCert       string `json:"ca_cert"`
		Client       string `json:"client"`
		ClientSecret string `json:"client_secret"`
		Target       string `json:"target"`
	} `json:"bosh"`
	Stemcellpath       string `json:"stemcell_path"`
	StemcellOs         string `json:"stemcell_os"`
	Az                 string `json:"az"`
	VmType             string `json:"vm_type"`
	VmExtensions       string `json:"vm_extensions"`
	Network            string `json:"network"`
	SkipCleanup        bool   `json:"skip_cleanup"`
	MountEphemeralDisk bool   `json:"mount_ephemeral_disk"`
}

func NewConfig() (*Config, error) {
	configFilePath := os.Getenv("CONFIG_JSON")
	if configFilePath == "" {
		return nil, fmt.Errorf("invalid config file path: %v", configFilePath)
	}
	body, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("empty config file path: %v", configFilePath)
	}
	var config Config
	err = json.Unmarshal(body, &config)
	if err != nil {
		return nil, fmt.Errorf("unable to parse config file: %s: %s", err.Error(), string(body))
	}
	if config.StemcellOs == "" {
		return nil, fmt.Errorf("missing required field: %v", "stemcell_os")
	}

	if config.VmExtensions == "" {
		config.VmExtensions = "500GB_ephemeral_disk"
	}

	return &config, nil
}

type BoshCommand struct {
	DirectorIP   string
	Client       string
	ClientSecret string
	CertPath     string // Path to CA CERT file, if any
	Timeout      time.Duration
	GwPrivateKeyPath string // Path to key file
	GwUser           string
}

func setupBosh(config *Config) *BoshCommand {
	var boshCertPath string
	cert := config.Bosh.CaCert
	if cert != "" {
		certFile, err := ioutil.TempFile("", "")
		Expect(err).To(Succeed())

		_, err = certFile.Write([]byte(cert))
		Expect(err).To(Succeed())

		boshCertPath, err = filepath.Abs(certFile.Name())
		Expect(err).To(Succeed())
	}

	timeout := BOSH_TIMEOUT
	var err error
	if s := os.Getenv("BWATS_BOSH_TIMEOUT"); s != "" {
		timeout, err = time.ParseDuration(s)
		log.Printf("Using BWATS_BOSH_TIMEOUT (%s) as timeout\n", s)

		if err != nil {
			log.Printf("Error parsing BWATS_BOSH_TIMEOUT (%s): %s - falling back to default\n", s, err)
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

func (c *BoshCommand) RunInStdOut(command, dir string) ([]byte, error) {
	cmd := exec.Command("bosh", c.args(command)...)
	if dir != "" {
		cmd.Dir = dir
		log.Printf("\nRUNNING %q IN %q\n", strings.Join(cmd.Args, " "), dir)
	} else {
		log.Printf("\nRUNNING %q\n", strings.Join(cmd.Args, " "))
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
		return stdout, fmt.Errorf("Non-zero exit code for cmd %q: %d\nSTDERR:\n%s\nSTDOUT:%s\n",
			strings.Join(cmd.Args, " "), exitCode, stderr, stdout)
	}
	return stdout, nil
}

func (c *BoshCommand) RunIn(command, dir string) error {
	_, err := c.RunInStdOut(command, dir)
	return err
}


func runTest(testName string) error {
	return bosh.Run(fmt.Sprintf("-d %s run-errand --download-logs %s --tty", deploymentName, testName))
}

func uploadStemcell(config *Config, bosh *BoshCommand) {
	matches, err := filepath.Glob(config.Stemcellpath)
	Expect(err).To(Succeed())
	Expect(matches).To(HaveLen(1))
	err = bosh.Run(fmt.Sprintf("upload-stemcell %s", matches[0]))
	if err != nil {
		// AWS takes a while to distribute the AMI across accounts
		time.Sleep(2 * time.Minute)
	}
	Expect(err).To(Succeed())
}

func createBwatsRelease(bosh *BoshCommand) string {
	pwd, err := os.Getwd()
	Expect(err).To(Succeed())

	releaseVersion = fmt.Sprintf("0.dev+%d", getTimestampInMs())
	goZipPath, err := downloadFile("golang-", GolangURL)
	Expect(err).To(Succeed())
	releaseDir := filepath.Join(pwd, "assets", "bwats-release")
	Expect(bosh.RunIn(fmt.Sprintf("add-blob %s golang-windows/%s", goZipPath, GoZipFile), releaseDir)).To(Succeed())
	mbsaMsiPath, err := downloadFile("mbsa-", MbsaURL)
	Expect(err).To(Succeed())
	Expect(bosh.RunIn(fmt.Sprintf("add-blob %s mbsa/%s", mbsaMsiPath, MbsaFile), releaseDir)).To(Succeed())
	Expect(bosh.RunIn(fmt.Sprintf("create-release --force --version %s", releaseVersion), releaseDir)).To(Succeed())
	Expect(bosh.RunIn("upload-release", releaseDir)).To(Succeed())

	return releaseVersion
}

func (m ManifestProperties) toVarsString() string {
	manifest := m.toMap()

	fmtString := "-v %s=%s "

	var b bytes.Buffer

	for k, v := range manifest {
		if v != "" {
			fmt.Fprintf(&b, fmtString, k, v)
		}
	}

	fmt.Fprintf(&b, "-v MountEphemeralDisk=%t", m.MountEphemeralDisk)

	return b.String()
}

func (m ManifestProperties) toMap() map[string]string {
	manifest := make(map[string]string)

	manifest["DeploymentName"] = m.DeploymentName
	manifest["ReleaseName"] = m.ReleaseName
	manifest["AZ"] = m.AZ
	manifest["VmType"] = m.VmType
	manifest["VmExtensions"] = m.VmExtensions
	manifest["Network"] = m.Network
	manifest["StemcellOs"] = m.StemcellOs
	manifest["StemcellVersion"] = m.StemcellVersion
	manifest["ReleaseVersion"] = m.ReleaseVersion

	return manifest
}

func downloadLogs(instanceName string, jobName string, index int, bosh *BoshCommand) *gbytes.Buffer {
	tempDir, err := ioutil.TempDir("", "")
	Expect(err).To(Succeed())
	defer os.RemoveAll(tempDir)

	err = bosh.Run(fmt.Sprintf("-d %s logs %s/%d --dir %s", deploymentName, instanceName, index, tempDir))
	Expect(err).To(Succeed())

	matches, err := filepath.Glob(filepath.Join(tempDir, fmt.Sprintf("%s.%s.%d-*.tgz", deploymentName, instanceName, index)))
	Expect(err).To(Succeed())
	Expect(matches).To(HaveLen(1))

	cmd := exec.Command("tar", "xf", matches[0], "-O", fmt.Sprintf("./%s/%s/job-service-wrapper.out.log", jobName, jobName))
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).To(Succeed())

	return session.Wait().Out
}

func getTimestampInMs() int64 {
	return time.Now().UTC().UnixNano() / int64(time.Millisecond)
}

func fetchStemcellInfo(stemcellPath string) (StemcellYML, error) {
	var stemcellInfo StemcellYML
	tempDir, err := ioutil.TempDir("", "")
	Expect(err).To(Succeed())
	defer os.RemoveAll(tempDir)

	cmd := exec.Command("tar", "xf", stemcellPath, "-C", tempDir, "stemcell.MF")
	session, err := gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).To(Succeed())
	Eventually(session, 20 * time.Minute).Should(gexec.Exit())

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

	stemcellMF, err := ioutil.ReadFile(fmt.Sprintf("%s/%s", tempDir, "stemcell.MF"))
	Expect(err).To(Succeed())

	err = yaml.Unmarshal(stemcellMF, &stemcellInfo)
	Expect(err).To(Succeed())
	Expect(stemcellInfo.Version).ToNot(BeNil())
	Expect(stemcellInfo.Version).ToNot(BeEmpty())

	return stemcellInfo, nil
}

func downloadFile(prefix, sourceUrl string) (string, error) {
	tempfile, err := ioutil.TempFile("", prefix)
	if err != nil {
		return "", err
	}

	filename := tempfile.Name()
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		return "", err
	}
	defer f.Close()

	res, err := http.Get(sourceUrl)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if _, err := io.Copy(f, res.Body); err != nil {
		return "", err
	}

	return filename, nil
}

func (c *Config) deployWithManifest(bosh *BoshCommand, deploymentName string, stemcellVersion string, bwatsVersion string, manifestPath string) error {
	manifestProperties := ManifestProperties{
		DeploymentName:     deploymentName,
		ReleaseName:        "bwats-release",
		AZ:                 c.Az,
		VmType:             c.VmType,
		VmExtensions:       c.VmExtensions,
		Network:            c.Network,
		StemcellOs:         c.StemcellOs,
		StemcellVersion:    fmt.Sprintf(`"%s"`, stemcellVersion),
		ReleaseVersion:     bwatsVersion,
		MountEphemeralDisk: c.MountEphemeralDisk,
	}

	err := bosh.Run(fmt.Sprintf("-d %s deploy %s %s", deploymentName, manifestPath, manifestProperties.toVarsString()))
	Expect(err).To(Succeed())

	return nil
}

func (c *Config) deploy(bosh *BoshCommand, deploymentName string, stemcellVersion string, bwatsVersion string) error {
	pwd, err := os.Getwd()
	Expect(err).To(Succeed())
	manifestPath = filepath.Join(pwd, "assets", "manifest.yml")

	return c.deployWithManifest(bosh, deploymentName, stemcellVersion, bwatsVersion, manifestPath)
}

func doSSHLogin(targetIP string) *gexec.Session {
	sshLoginDone := make(chan bool, 1)
	var session *gexec.Session

	go func() {
		defer GinkgoRecover()

		directorAddress := strings.Split(bosh.DirectorIP, ":")[0]

		var err error
		session, err = runCommand("ssh", "-nNT", fmt.Sprintf("%s@%s", bosh.GwUser, directorAddress), "-i", bosh.GwPrivateKeyPath, "-L", fmt.Sprintf("3389:%s:3389", targetIP), "-o", "StrictHostKeyChecking=no", "-o", "ExitOnForwardFailure=yes")
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(5 * time.Second)

		sshLoginDone <- true
	}()

	<-sshLoginDone

	return session
}

func runCommand(cmd string, args ...string) (*gexec.Session, error) {
	return gexec.Start(exec.Command(cmd, args...), GinkgoWriter, GinkgoWriter)
}

type vmInfo struct {
	Tables []struct {
		Rows []struct {
			Instance string `json:"instance"`
			IPs      string `json:"ips"`
		} `json:"Rows"`
	} `json:"Tables"`
}

func getFirstInstanceIP(deployment string, instanceName string) (string, error) {
	var vms vmInfo
	stdout, err := bosh.RunInStdOut(fmt.Sprintf("vms -d %s --json", deployment), "")
	if err != nil {
		return "", err
	}

	if err = json.Unmarshal(stdout, &vms); err != nil {
		return "", err
	}

	for _, row := range vms.Tables[0].Rows {
		if strings.HasPrefix(row.Instance, instanceName) {
			ips := strings.Split(row.IPs, "\n")
			if len(ips) == 0 {
				break
			}
			return ips[0], nil
		}
	}

	return "", errors.New("No instance IPs found!")
}
