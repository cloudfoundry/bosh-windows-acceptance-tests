package bosh_windows_acceptance_tests_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(GinkgoWriter)
}

var (
	bosh                      *BoshCommand
	deploymentName            string
	manifestPath              string
	stemcellName              string
	stemcellVersion           string
	releaseVersion            string
	tightLoopStemcellVersions []string
	config                    *Config
	deploymentNameRDP     string
)

var _ = Describe("BOSH Windows", func() {
	BeforeSuite(func() {
		var err error

		config, err = NewConfig()
		Expect(err).NotTo(HaveOccurred())
		bosh = setupBosh(config)

		bosh.Run("login")
		deploymentName = fmt.Sprintf("windows-acceptance-test-%d", getTimestampInMs())

		stemcellYML, err := fetchStemcellInfo(config.Stemcellpath)
		Expect(err).To(Succeed())

		stemcellName = stemcellYML.Name
		stemcellVersion = stemcellYML.Version

		releaseVersion = createBwatsRelease(bosh)

		uploadStemcell(config, bosh)

		err = config.deploy(bosh, deploymentName, stemcellVersion, releaseVersion)
		Expect(err).To(Succeed())
	})

	AfterSuite(func() {
		// Delete the releases created by the tight loop test
		for index , version := range tightLoopStemcellVersions {
			if index == len(tightLoopStemcellVersions) - 1 {
				continue // Last release is still being used by the deployment, so it cannot be deleted yet
			}
			bosh.Run(fmt.Sprintf("delete-release bwats-release/%s", version))
		}
		if config.SkipCleanup {
			return
		}

		bosh.Run(fmt.Sprintf("-d %s delete-deployment --force", deploymentName))
		bosh.Run(fmt.Sprintf("delete-stemcell %s/%s", stemcellName, stemcellVersion))
		bosh.Run(fmt.Sprintf("delete-release bwats-release/%s", releaseVersion))
		bosh.Run(fmt.Sprintf("delete-release bwats-release/%s", tightLoopStemcellVersions[len(tightLoopStemcellVersions) - 1]))

		if bosh.CertPath != "" {
			os.RemoveAll(bosh.CertPath)
		}
	})

	It("can run a job that relies on a package", func() {
		time.Sleep(60 * time.Second)
		Eventually(downloadLogs("check-multiple", "simple-job", 0, bosh),
			time.Second*65).Should(gbytes.Say("60 seconds passed"))
	})

	It("successfully runs redeploy in a tight loop", func() {
		pwd, err := os.Getwd()
		Expect(err).To(BeNil())
		releaseDir := filepath.Join(pwd, "assets", "bwats-release")

		f, err := os.OpenFile(filepath.Join(releaseDir, "jobs", "simple-job", "templates", "pre-start.ps1"),
			os.O_APPEND|os.O_WRONLY, 0600)
		Expect(err).ToNot(HaveOccurred())
		defer f.Close()

		for i := 0; i < redeployRetries; i++ {
			log.Printf("Redeploy attempt: #%d\n", i)

			version := fmt.Sprintf("0.dev+%d", getTimestampInMs())
			tightLoopStemcellVersions = append(tightLoopStemcellVersions, version)
			Expect(bosh.RunIn("create-release --force --version "+version, releaseDir)).To(Succeed())

			Expect(bosh.RunIn("upload-release", releaseDir)).To(Succeed())

			err = config.deploy(bosh, deploymentName, stemcellVersion, version)

			if err != nil {
				downloadLogs("check-multiple", "simple-job", 0, bosh)
				Fail(err.Error())
			}
		}
	})

	It("checks system dependencies and security, auto update has turned off, currently has a Service StartType of 'Manual' and initially had a StartType of 'Delayed', and password is randomized", func() {
		err := runTest("check-system")
		Expect(err).To(Succeed())
	})

	It("is fully updated", func() { // 860s
		err := runTest("check-updates")
		Expect(err).To(Succeed())
	})

	It("mounts ephemeral disks when asked to do so and does not mount them otherwise", func() {
		err := runTest("ephemeral-disk")
		Expect(err).To(Succeed())
	})

	Context("Rdp", func() {
		instanceName     := "check-multiple"
		username         := "Administrator"
		password := "no-idea"

		It("RDP is disabled by default", func() {
			instanceIP, err := getFirstInstanceIP(deploymentName, instanceName)
			Expect(err).NotTo(HaveOccurred())

			disabledSession := doSSHLogin(instanceIP)
			Eventually(disabledSession).Should(gexec.Exit())
			Eventually(disabledSession.Err).Should(gbytes.Say(`Could not request local forwarding.`))

			Eventually(func() (*gexec.Session, error) {
				rdpSession, err := runCommand("/bin/bash", "-c", fmt.Sprintf("xfreerdp /cert-ignore /u:%s /p:'%s' /v:localhost:3389 +auth-only", username, password))
				Eventually(rdpSession, 30*time.Second).Should(gexec.Exit())

				return rdpSession, err
			}, 3*time.Minute).Should(gexec.Exit(0))


		})
	})

	Context("slow compiling go package", func() {
		var slowCompilingDeploymentName string

		AfterEach(func() {
			bosh.Run(fmt.Sprintf("-d %s delete-deployment --force", slowCompilingDeploymentName))
		})

		It("deploys when there is a slow to compile go package", func() {
			pwd, err := os.Getwd()
			Expect(err).To(Succeed())
			manifestPath = filepath.Join(pwd, "assets", "slow-compile-manifest.yml")

			slowCompilingDeploymentName = fmt.Sprintf("windows-acceptance-test-slow-compile-%d", getTimestampInMs())

			config.deployWithManifest(bosh, slowCompilingDeploymentName, stemcellVersion, releaseVersion, manifestPath)
		})
	})
})
