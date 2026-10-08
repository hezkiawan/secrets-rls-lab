# demo.ps1 — short commands for recording the video demo (PowerShell, from the repo root).
#
#   Set-ExecutionPolicy -Scope Process Bypass    # lets this window run .ps1 files
#   . .\reference\scripts\demo.ps1               # a dot, a space, then the path
#
# Every function is a thin wrapper around a command we already use (docker compose, bao, curl, psql).
# No function prints a token: tokens are read from reference/.secrets/*.json (LAB ONLY) only when needed.

# docker compose for the reference stack
function dc { docker compose -f reference/docker-compose.yml @args }

# --- tokens (read from the lab files, never printed) -------------------------------------
function Get-RootToken {
  if (Test-Path reference/.secrets/init.json) { (Get-Content reference/.secrets/init.json -Raw | ConvertFrom-Json).root_token } else { "" }
}
function Get-UnsealerToken {
  if (Test-Path reference/.secrets/unsealer-init.json) { (Get-Content reference/.secrets/unsealer-init.json -Raw | ConvertFrom-Json).root_token } else { "" }
}
# For a UI login: copies the token, you paste it into the (masked) token field.
function Copy-RootToken     { Get-RootToken | Set-Clipboard;     "cluster root token copied to the clipboard" }
function Copy-UnsealerToken { Get-UnsealerToken | Set-Clipboard; "unsealer root token copied to the clipboard" }

# --- the bao CLI -------------------------------------------------------------------------
# Runs bao INSIDE one node, talking to that node only, as admin.   Example: bao-on bao-1 status
# (-e BAO_TOKEN overrides the node's own BAO_TOKEN, which is the unseal token for the transit seal.)
function bao-on {
  param($Node)
  dc exec -T -e BAO_ADDR=http://127.0.0.1:8200 -e "BAO_TOKEN=$(Get-RootToken)" $Node bao @args
}
# Same, inside the unsealer.   Example: bao-unsealer token lookup lab-transit-unseal-token
function bao-unsealer {
  dc exec -T -e BAO_ADDR=http://127.0.0.1:8200 -e "BAO_TOKEN=$(Get-UnsealerToken)" unsealer bao @args
}

# --- cluster views -----------------------------------------------------------------------
# Which node is the leader? Asks each node's health endpoint (200 = active).
function Get-Leader {
  foreach ($p in 8201, 8202, 8203) {
    if ((curl.exe -s -o NUL -w "%{http_code}" "http://localhost:$p/v1/sys/health") -eq "200") { "bao-$($p - 8200)" }
  }
}
# Sealed? Leader or standby? Raft committed index? — for all three nodes.
function Show-Raft {
  foreach ($n in "bao-1", "bao-2", "bao-3") {
    "== $n"
    bao-on $n status | Select-String "Sealed|HA Mode|Raft Committed Index"
  }
}
# What the apps see: the load balancer's health answer, once per second. Ctrl+C to stop.
function Watch-Health {
  while ($true) {
    $code = curl.exe -s -o NUL -w "%{http_code}" http://localhost:8300/v1/sys/health
    "{0}  :8300 -> {1}" -f (Get-Date -Format "HH:mm:ss"), $code
    Start-Sleep 1
  }
}

# --- Postgres statement log (LIVE 7) -----------------------------------------------------
# Turn on: log every statement + the user who ran it. Turn OFF after recording.
function Pg-LogOn {
  dc exec -T postgres psql -U postgres -c "ALTER SYSTEM SET log_statement = 'all'" -c "ALTER SYSTEM SET log_line_prefix = '%m %u '" -c "SELECT pg_reload_conf()"
}
function Pg-LogOff {
  dc exec -T postgres psql -U postgres -c "ALTER SYSTEM RESET log_statement" -c "ALTER SYSTEM RESET log_line_prefix" -c "SELECT pg_reload_conf()"
}
# Only the lines that matter for RLS: BEGIN, set_config (+ its parameters), the query, COMMIT. Ctrl+C to stop.
function Watch-PgLog {
  dc logs -f --since 1s postgres | Select-String "begin|set_config|parameters|support.conversations|commit"
}

"demo helpers loaded: dc, bao-on, bao-unsealer, Get-Leader, Show-Raft, Watch-Health, Copy-RootToken, Copy-UnsealerToken, Pg-LogOn/Off, Watch-PgLog"
