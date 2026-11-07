# Security

Signal Atlas collects over SSH, Telnet, SNMP and HTTPS and stores credentials for those devices.
Please report vulnerabilities privately (open a draft security advisory on the repository) instead
of a public issue.

- Start from a trusted network: the app has a single operator account and no multi-tenant model.
- Device passwords are encrypted with `SIGNAL_ATLAS_SECRET_KEY` when it is set; without it they only
  live in process memory and must be re-entered after a restart.
- SSH host keys are verified against `known_hosts`. A new host is only trusted after an operator
  approves its SHA256 fingerprint in the UI.
- Never commit `.env`, `runtime/`, or `data/` — they contain credentials and history.
