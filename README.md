# bosh-windows-acceptance-tests

This repo houses tests used to verify Windows Stemcells function as expected.

# Example configuration


These tests are run in the [BOSH Windows 2019 Stemcells Pipeline](https://ci.bosh-ecosystem.cf-app.com/).

You can create a `config.json` file, eg:

```json
{
  "bosh": {
    "ca_cert": "<contents of your bosh director cert, with \n for newlines>",
    "client": "<bosh client name>",
    "client_secret": "<bosh client secret>",
    "target": "<IP of your bosh director>"
  },
  "stemcell_path": "<absolute path to stemcell tgz>",
  "stemcell_os": "<stemcell OS either (windowsYYYY)>",
  "az": "<area zone from bosh cloud config>",
  "vm_type": "<vm_type from bosh cloud config>",
  "vm_extensions": "<comma separated string of options, e.g. 50GB_ephemeral_disk>",
  "network": "<network from bosh cloud config>",
  "skip_cleanup": "<skip cleanup - if this is false all unused stemcells are deleted>"
  "skip_ms_update_test": "<skip check-updates errand - if true, it will not test that all Windows updates are installed>",
  "ssh_disabled_by_default": "check ssh daemon default startup type - if true then it checks that the startup type is DISABLED. If false or missing, checks startup type is AUTOMATIC",
  "security_compliance_applied": "check that Microsoft Baseline policies have been applied"
}
```

And then run these tests with `CONFIG_JSON=<path-to-config.json> ginkgo`.

The timeout for BOSH commands can be overridden with the BWATS_BOSH_TIMEOUT environment variable.

# Release dependencies

## LGPO

- Download LGPO.zip from the [Microsoft Security Compliance Toolkit](https://www.microsoft.com/en-us/download/details.aspx?id=55319)
- Unzip LGPO
- Add LGPO.exe as a bosh blob locally: `bosh add-blob <PATH TO UNZIP LOCATION>/LGPO.exe lgpo/LGPO.exe`

## Go

- Download the Go version specified in the `go.mod` file from the [Go releases page](https://go.dev/dl/)
- Add file as a bosh blob locally `bosh add-blob <PATH TO DOWNLOAD LOCATION>/go1.12.7.windows-amd64.zip golang-windows/go1.12.7.windows-amd64.zip`


# Internals of the release and what it does
This release has a few tests that verify if the features are installed on stemcell or not. There are few jobs on the BWATS release at [assets/bwats-release/jobs path](https://github.com/cloudfoundry/bosh-windows-acceptance-tests/tree/master/assets/bwats-release/jobs.

When ginkgo tests are run, these jobs are installed on the stemcells and the tests in each job are run against it. As part of running BWATs there are several deployments done, to avoid conficts on same stemcell. 

The bulk of our assertions are run as part of the check-system job.
**This is where you will likely want to make changes**, e.g. to add test coverage around our expectations of happy-path stemcell behavior.

The relevant call chain is:
- CI, (or you, locally), runs `ginkgo .`
- ginkgo runs `main_test.go`
- the "check system dep..." test runs the `check-system` bosh errand, whose behavior is defined in `assets/bwats-release/jobs/check-system/spec` and `assets/bwats-release/jobs/check-system/templates/run.ps1.erb`
- the run.ps1 script defines test functions, and runs them. e.g. `Verify-Dependencies` is defined and run in that run.ps1

