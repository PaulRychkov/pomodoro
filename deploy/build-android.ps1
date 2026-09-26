$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

$tools = "$env:USERPROFILE\android-tools"
$env:JAVA_HOME = (Get-ChildItem "$tools\jdk-extract" -Directory | Sort-Object Name -Descending | Select-Object -First 1).FullName
$env:ANDROID_HOME = "$tools\sdk"
$env:ANDROID_NDK_HOME = (Get-ChildItem "$tools\sdk\ndk" -Directory | Sort-Object Name -Descending | Select-Object -First 1).FullName
$env:PATH = "$env:JAVA_HOME\bin;$env:USERPROFILE\go\bin;$env:PATH"
$env:GOFLAGS = "-p=1"
$gradle = (Get-ChildItem "$tools\gradle-*\bin\gradle.bat" | Sort-Object FullName -Descending | Select-Object -First 1).FullName
foreach ($p in @($env:JAVA_HOME, $env:ANDROID_NDK_HOME, $gradle)) {
    if (-not $p -or -not (Test-Path $p)) { throw "не найдено в $tools — поставь android-окружение" }
}

$versionFile = "$repo\android\version.properties"
$buildNumber = 1
if (Test-Path $versionFile) {
    $current = (Select-String -Path $versionFile -Pattern '^build=(\d+)').Matches.Groups[1].Value
    if ($current) { $buildNumber = [int]$current + 1 }
}
Set-Content -Path $versionFile -Value "build=$buildNumber" -Encoding ascii
$version = "1.0.$buildNumber"
Write-Host "== версия $version =="

Write-Host "== frontend =="
Push-Location "$repo\frontend"
npm run build
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "npm run build failed" }
Pop-Location

Write-Host "== webdist =="
$webdist = "$repo\mobile\webdist"
Get-ChildItem $webdist -Force | Where-Object { $_.Name -ne '.gitignore' } | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item "$repo\frontend\dist\*" $webdist -Recurse -Force

Write-Host "== gomobile bind =="
Push-Location $repo
New-Item -ItemType Directory -Force "$repo\android\app\libs" | Out-Null
gomobile bind "-target=android/arm64,android/amd64" -androidapi 24 -o "$repo\android\app\libs\pomodoro.aar" ./mobile
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "gomobile bind failed" }
Pop-Location

Write-Host "== gradle =="
& $gradle -p "$repo\android" assembleRelease
if ($LASTEXITCODE -ne 0) { throw "gradle failed" }

$built = "$repo\android\app\build\outputs\apk\release\app-release.apk"
$target = Join-Path ([Environment]::GetFolderPath('Desktop')) "Помидор $version.apk"
Get-ChildItem ([Environment]::GetFolderPath('Desktop')) -Filter 'Помидор *.apk' | Remove-Item -Force
Copy-Item $built $target -Force
Write-Host "APK: $target"
