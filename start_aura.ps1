# Setup Environment for Go + CGO
$compilerPath = "C:\Users\akatre\AppData\Local\Microsoft\WinGet\Packages\MartinStorsjo.LLVM-MinGW.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\llvm-mingw-20260421-ucrt-x86_64\bin"
$env:Path = "$compilerPath;" + $env:Path
$env:CGO_ENABLED = "1"

Write-Host "--- Starting Aura Voice Assistant ---" -ForegroundColor Cyan
& "C:\Program Files\Go\bin\go.exe" run .
