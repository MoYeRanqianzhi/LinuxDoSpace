[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$CloudflareApiToken,

    [Parameter(Mandatory = $true)]
    [string]$CloudflareAccountId,

    [Parameter(Mandatory = $true)]
    [string]$ParentZoneId,

    [Parameter(Mandatory = $true)]
    [string]$TargetEmail,

    [Parameter()]
    [string]$ParentZoneName = "linuxdo.space",

    [Parameter()]
    [string]$ChildLabel = "test",

    [Parameter()]
    [int]$ActivationPollCount = 20,

    [Parameter()]
    [int]$ActivationPollSeconds = 15
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Net.Http
$script:CloudflareHttpClient = New-Object System.Net.Http.HttpClient
$script:CloudflareHttpClient.Timeout = [TimeSpan]::FromSeconds(60)

# Write-Step keeps the probe output easy to follow when the script is used as a
# manual validation tool before backend development begins.
function Write-Step {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Message
    )

    Write-Host ""
    Write-Host ("==> " + $Message)
}

# Invoke-CloudflareRequest sends one authenticated request to the public
# Cloudflare API and returns the parsed JSON body. Errors are rethrown with the
# original Cloudflare payload preserved whenever possible.
function Invoke-CloudflareRequest {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Method,

        [Parameter(Mandatory = $true)]
        [string]$Uri,

        [Parameter()]
        $Body
    )

    $headers = @{
        Authorization = "Bearer $CloudflareApiToken"
    }

    $request = New-Object System.Net.Http.HttpRequestMessage ([System.Net.Http.HttpMethod]::new($Method), $Uri)
    $request.Headers.Authorization = New-Object System.Net.Http.Headers.AuthenticationHeaderValue("Bearer", $CloudflareApiToken)

    if ($null -ne $Body) {
        $payload = $Body | ConvertTo-Json -Depth 20
        $request.Content = New-Object System.Net.Http.StringContent($payload, [System.Text.Encoding]::UTF8, "application/json")
    }

    try {
        $response = $script:CloudflareHttpClient.SendAsync($request).GetAwaiter().GetResult()
    }
    catch {
        throw "Cloudflare API $Method $Uri failed before a response was received: $($_.Exception.Message)"
    }

    $rawContent = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    if ([string]::IsNullOrWhiteSpace($rawContent)) {
        throw "Cloudflare API $Method $Uri returned an empty body"
    }

    try {
        $parsed = $rawContent | ConvertFrom-Json
    }
    catch {
        throw "Cloudflare API $Method $Uri returned non-JSON content: $rawContent"
    }

    if ($parsed.PSObject.Properties.Name -contains "success" -and -not $parsed.success) {
        $messages = @()

        if ($parsed.PSObject.Properties.Name -contains "errors" -and $null -ne $parsed.errors) {
            foreach ($apiError in $parsed.errors) {
                $message = ""
                if ($apiError.PSObject.Properties.Name -contains "message") {
                    $message = [string]$apiError.message
                }
                if ([string]::IsNullOrWhiteSpace($message)) {
                    $message = $apiError | ConvertTo-Json -Compress -Depth 20
                }
                $messages += $message
            }
        }

        if ($messages.Count -eq 0) {
            $messages += "Cloudflare returned success=false without detailed errors"
        }

        throw "Cloudflare API $Method $Uri failed: $($messages -join '; ')"
    }

    return $parsed
}

# Get-AllDestinationAddresses walks Cloudflare pagination so the probe never
# mistakes a later-page verified destination address for a missing mailbox.
function Get-AllDestinationAddresses {
    $all = @()
    $page = 1

    while ($true) {
        $listUri = "https://api.cloudflare.com/client/v4/accounts/$CloudflareAccountId/email/routing/addresses?page=$page&per_page=100"
        $list = Invoke-CloudflareRequest -Method GET -Uri $listUri
        if (-not $list.success) {
            throw "failed to list Cloudflare Email Routing destination addresses"
        }

        $all += @($list.result)
        $totalPages = [int]$list.result_info.total_pages
        if ($totalPages -le 0 -or $page -ge $totalPages) {
            break
        }

        $page++
    }

    return $all
}

# Get-ExistingChildZone returns the current child-zone object when the account
# already has the requested zone. The probe reuses it instead of blindly
# creating duplicates.
function Get-ExistingChildZone {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ChildZoneName
    )

    $uri = "https://api.cloudflare.com/client/v4/zones?name=$ChildZoneName"
    $response = Invoke-CloudflareRequest -Method GET -Uri $uri
    if (-not $response.success) {
        throw "failed to query existing child zone $ChildZoneName"
    }

    if ($response.result.Count -eq 0) {
        return $null
    }

    return $response.result[0]
}

# Ensure-ParentDelegation writes the child-zone NS records into the parent zone.
# Any pre-existing NS records for the exact child name are replaced to keep the
# delegation deterministic.
function Ensure-ParentDelegation {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ChildZoneName,

        [Parameter(Mandatory = $true)]
        [string[]]$NameServers
    )

    $lookupUri = "https://api.cloudflare.com/client/v4/zones/$ParentZoneId/dns_records?name=$ChildZoneName&per_page=100"
    $lookup = Invoke-CloudflareRequest -Method GET -Uri $lookupUri
    if (-not $lookup.success) {
        throw "failed to load parent-zone records for $ChildZoneName"
    }

    foreach ($record in $lookup.result) {
        if ($record.type -ne "NS") {
            throw "parent zone already contains non-NS record $($record.type) at $ChildZoneName; delegation cannot proceed safely"
        }
    }

    foreach ($record in $lookup.result) {
        $deleteUri = "https://api.cloudflare.com/client/v4/zones/$ParentZoneId/dns_records/$($record.id)"
        $deleteResult = Invoke-CloudflareRequest -Method DELETE -Uri $deleteUri
        if (-not $deleteResult.success) {
            throw "failed to delete stale parent NS record $($record.id)"
        }
    }

    foreach ($nameServer in $NameServers) {
        $createUri = "https://api.cloudflare.com/client/v4/zones/$ParentZoneId/dns_records"
        $body = @{
            type    = "NS"
            name    = $ChildZoneName
            content = $nameServer
            ttl     = 3600
        }

        $create = Invoke-CloudflareRequest -Method POST -Uri $createUri -Body $body
        if (-not $create.success) {
            throw "failed to create parent NS delegation for $ChildZoneName -> $nameServer"
        }
    }
}

# Wait-ForZoneActivation polls the child zone until Cloudflare marks it active
# after parent NS delegation has propagated.
function Wait-ForZoneActivation {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ChildZoneId
    )

    for ($attempt = 1; $attempt -le $ActivationPollCount; $attempt++) {
        $zone = Invoke-CloudflareRequest -Method GET -Uri "https://api.cloudflare.com/client/v4/zones/$ChildZoneId"
        if (-not $zone.success) {
            throw "failed to poll child zone activation state"
        }

        $status = [string]$zone.result.status
        Write-Host ("Activation poll {0}/{1}: {2}" -f $attempt, $ActivationPollCount, $status)

        if ($status -eq "active") {
            return $zone.result
        }

        Start-Sleep -Seconds $ActivationPollSeconds
    }

    throw "child zone did not become active within the configured polling window"
}

# Ensure-VerifiedDestination makes sure the requested forwarding target already
# exists as a verified Email Routing destination address. If it does not exist,
# the script creates it and then stops so the operator can click the verification
# email before rerunning the probe.
function Ensure-VerifiedDestination {
    param(
        [Parameter(Mandatory = $true)]
        [string]$EmailAddress
    )

    $listUri = "https://api.cloudflare.com/client/v4/accounts/$CloudflareAccountId/email/routing/addresses"
    $existing = Get-AllDestinationAddresses | Where-Object { $_.email -eq $EmailAddress } | Select-Object -First 1
    if ($null -eq $existing) {
        Write-Step "Creating destination address $EmailAddress"
        $create = Invoke-CloudflareRequest -Method POST -Uri $listUri -Body @{ email = $EmailAddress }
        if (-not $create.success) {
            throw "failed to create destination address $EmailAddress"
        }

        throw "destination address $EmailAddress was created but is not yet verified; click the verification email and rerun the probe"
    }

    if ([string]::IsNullOrWhiteSpace([string]$existing.verified)) {
        throw "destination address $EmailAddress exists but is not verified; click the verification email and rerun the probe"
    }

    return $existing
}

$childZoneName = "$ChildLabel.$ParentZoneName"

Write-Step "Verifying Cloudflare token"
$tokenInfo = Invoke-CloudflareRequest -Method GET -Uri "https://api.cloudflare.com/client/v4/user/tokens/verify"
if (-not $tokenInfo.success) {
    throw "Cloudflare token verification failed"
}

Write-Step "Resolving child zone $childZoneName"
$childZone = Get-ExistingChildZone -ChildZoneName $childZoneName
if ($null -eq $childZone) {
    Write-Step "Creating child zone $childZoneName"
    $childZone = Invoke-CloudflareRequest -Method POST -Uri "https://api.cloudflare.com/client/v4/zones" -Body @{
        name       = $childZoneName
        account    = @{ id = $CloudflareAccountId }
        jump_start = $false
        type       = "full"
    }

    if (-not $childZone.success) {
        throw "failed to create child zone $childZoneName"
    }

    $childZone = $childZone.result
}

if ($null -eq $childZone.name_servers -or $childZone.name_servers.Count -lt 2) {
    throw "child zone $childZoneName did not return delegated name servers"
}

Write-Step "Ensuring parent-zone NS delegation for $childZoneName"
Ensure-ParentDelegation -ChildZoneName $childZoneName -NameServers $childZone.name_servers

Write-Step "Waiting for child zone activation"
$activeZone = Wait-ForZoneActivation -ChildZoneId $childZone.id

Write-Step "Enabling Email Routing DNS for $childZoneName"
$enableResponse = Invoke-CloudflareRequest -Method POST -Uri "https://api.cloudflare.com/client/v4/zones/$($activeZone.id)/email/routing/dns"
if (-not $enableResponse.success) {
    throw "failed to enable Email Routing DNS for $childZoneName"
}

Write-Step "Ensuring verified destination address $TargetEmail"
$destination = Ensure-VerifiedDestination -EmailAddress $TargetEmail

Write-Step "Updating child-zone catch-all for $childZoneName"
$catchAll = Invoke-CloudflareRequest -Method PUT -Uri "https://api.cloudflare.com/client/v4/zones/$($activeZone.id)/email/routing/rules/catch_all" -Body @{
    actions  = @(
        @{
            type  = "forward"
            value = @($TargetEmail)
        }
    )
    matchers = @(
        @{
            type = "all"
        }
    )
    enabled  = $true
    name     = "Catch-all for $childZoneName"
}
if (-not $catchAll.success) {
    throw "failed to update child-zone catch-all for $childZoneName"
}

Write-Step "Loading final catch-all rule"
$finalCatchAll = Invoke-CloudflareRequest -Method GET -Uri "https://api.cloudflare.com/client/v4/zones/$($activeZone.id)/email/routing/rules/catch_all"
if (-not $finalCatchAll.success) {
    throw "failed to read back final catch-all rule for $childZoneName"
}

Write-Step "Probe completed"
[pscustomobject]@{
    ChildZoneName      = $childZoneName
    ChildZoneId        = $activeZone.id
    ChildZoneStatus    = $activeZone.status
    DelegatedNameSrv1  = $childZone.name_servers[0]
    DelegatedNameSrv2  = $childZone.name_servers[1]
    DestinationEmail   = $destination.email
    CatchAllRuleId     = $finalCatchAll.result.id
    CatchAllRuleName   = $finalCatchAll.result.name
    CatchAllTarget     = (($finalCatchAll.result.actions | Select-Object -First 1).value | Select-Object -First 1)
} | Format-List
