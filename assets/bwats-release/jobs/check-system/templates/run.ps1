# Verify LGPO
if ($windowsVersion -Match "2012") {
  echo "Verifying that expected policies have been applied"

  lgpo /b $PSScriptRoot
  $LgpoDir = "$PSScriptRoot\" + (Get-ChildItem $PSScriptRoot -Directory | ?{ $_.Name -match "{*}" } | select -First 1).Name

  $OutputDir="$PSScriptRoot\lgpo_test"
  mkdir $OutputDir

  lgpo /parse /m "$LgpoDir\DomainSysvol\GPO\Machine\registry.pol" > "$OutputDir\machine_registry.unedited.txt"
  Get-Content "$OutputDir\machine_registry.unedited.txt" | select -Skip 3 > "$OutputDir\machine_registry.txt"

  lgpo /parse /u "$LgpoDir\DomainSysvol\GPO\User\registry.pol" > "$OutputDir\user_registry.unedited.txt"
  Get-Content "$OutputDir\user_registry.unedited.txt" | select -Skip 3 > "$OutputDir\user_registry.txt"

  copy "$LgpoDir\DomainSysvol\GPO\Machine\microsoft\windows nt\Audit\audit.csv" "$OutputDir"
  $Csv     = Import-Csv "$LgpoDir\DomainSysvol\GPO\Machine\microsoft\windows nt\Audit\audit.csv"
  $Include = $Csv[0].psobject.properties | select -ExpandProperty Name -Skip 1
  $Csv | select $Include | export-csv "$OutputDir\audit.csv" -NoTypeInformation

  copy "$LgpoDir\DomainSysvol\GPO\Machine\microsoft\windows nt\SecEdit\GptTmpl.inf" "$OutputDir"

  function Assert-NoDiff {
    Param (
      [string] $fileA = (Throw "first filename param required"),
      [string] $fileB = (Throw "second filename param required")
    )
    fc.exe /t "$fileA" "$fileB"

    if ($LastExitCode -ne 0) {
      Write-Error "Expected no diff between $fileA and $fileB"
      Exit 1
    }
  }

  # Diff the files aginst the fixtures
  $TestDir = "$PSScriptRoot\..\test"

  Assert-NoDiff "$OutputDir\machine_registry.txt" "$TestDir\machine_registry.txt"
  Assert-NoDiff "$OutputDir\user_registry.txt" "$TestDir\user_registry.txt"
  Assert-NoDiff "$OutputDir\GptTmpl.inf" "$TestDir\GptTmpl.inf"
  Assert-NoDiff "$OutputDir\audit.csv" "$TestDir\audit.csv"
}

Exit 0
