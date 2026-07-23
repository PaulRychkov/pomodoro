$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot

$env:JAVA_HOME = "C:\Users\pavel\android-tools\jdk-extract\jdk-21.0.11+10"
$env:ANDROID_HOME = "C:\Users\pavel\android-tools\sdk"
$env:ANDROID_NDK_HOME = "C:\Users\pavel\android-tools\sdk\ndk\30.0.15729638"
$env:PATH = "$env:JAVA_HOME\bin;C:\Users\pavel\go\bin;$env:PATH"
$env:GOFLAGS = "-p=1"
$gradle = "C:\Users\pavel\android-tools\gradle-8.11.1\bin\gradle.bat"

Write-Host "== frontend =="
Push-Location "$repo\frontend"
npm run build
Pop-Location

Write-Host "== webdist =="
$webdist = "$repo\mobile\webdist"
Remove-Item "$webdist\*" -Recurse -Force -ErrorAction SilentlyContinue
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

Write-Host "APK: $repo\android\app\build\outputs\apk\release\app-release.apk"
