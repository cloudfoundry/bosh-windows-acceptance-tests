# Do not set ErrorActionPreference to stop as Get-Acl will error
# if we do not have permission to read file permissions.

# Check for dependencies

$BOSH_BIN="C:\\var\\vcap\\bosh\\bin"
Write-Host "Checking $BOSH_BIN dependencies"

$files = New-Object System.Collections.ArrayList
[void] $files.AddRange((
    "bosh-blobstore-s3.exe",
    "bosh-blobstore-dav.exe",
    "tar.exe",
    "zlib1.dll",
    "job-service-wrapper.exe"
))

Get-ChildItem $BOSH_BIN | ForEach-Object {
  Write-Host "Checking for $_.Name"
  $files.remove($_.Name)
}

If ($files.Count -gt 0) {
  Write-Error "Unable to find the following binaries: $($files -join ',')"
  Exit 1
}

# Check ACLs

$expectedacls = New-Object System.Collections.ArrayList
[void] $expectedacls.AddRange((
    "${env:COMPUTERNAME}\Administrator,Allow",
    "NT AUTHORITY\SYSTEM,Allow",
    "BUILTIN\Administrators,Allow",
    "CREATOR OWNER,Allow",
    "APPLICATION PACKAGE AUTHORITY\ALL APPLICATION PACKAGES,Allow"
))

function Check-Acls {
    param([string]$path)

    $errCount = 0

    Get-ChildItem -Path $path -Recurse | foreach {
        $name = $_.FullName
        If (-Not ($_.Attributes -match "ReparsePoint")) {
            Get-Acl $name | Select -ExpandProperty Access | ForEach-Object {
                $ident = ('{0},{1}' -f $_.IdentityReference, $_.AccessControlType).ToString()
                If (-Not $expectedacls.Contains($ident)) {
                    If (-Not ($ident -match "NT [\w]+\\[\w]+,Allow")) {
                        $errCount += 1
                        Write-Host "Error ($name): $ident"
                    }
                }
            }
        }
    }

    return $errCount
}

$errCount = 0
$errCount += Check-Acls "C:\var"
$errCount += Check-Acls "C:\bosh"
$errCount += Check-Acls "C:\Windows\Panther\Unattend"
if ($errCount -ne 0) {
    Write-Error "FAILED: $errCount"
    Exit 1
}

# Check WinRM
If ( (Get-Service WinRM).Status -ne "Stopped") {
  $msg = "WinRM is not Stopped. It is {0}" -f $(Get-Service WinRM).Status
  Write-Error $msg
  Exit 1
}

Exit 0