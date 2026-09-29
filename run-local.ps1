<#
.SYNOPSIS
    Convenience wrapper to run OENEXA locally.
#>
[CmdletBinding()]
param(
    [switch]$Reset,
    [switch]$Rebuild,
    [switch]$NoMiner,
    [int]$Port = 8545,
    [int]$MetricsPort = 9090,
    [string]$Password = "devpassword"
)

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
& (Join-Path $ScriptDir "scripts\run-local.ps1") @PSBoundParameters
