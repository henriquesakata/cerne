#!/usr/bin/env sh
# F-0103 — provenance capture: after an agent records or advances a decision
# gate, remind it to register the external sources that SHAPED that decision
# (only the useful ones) as cerne references — so provenance is captured without
# waiting to be asked, and dead-ends are never saved.
#
# PostToolUse hook (matcher Bash). Inspects ONLY tool_input.command (never the
# tool_response output — else a command that merely PRINTS a gate phrase, e.g.
# reading this skill, would misfire) and reacts ONLY on a decision-recording gate
# (cerne feature approve/complete, cerne decision new/substituir) — the moment a
# source's usefulness is finally known. Emits a non-blocking additionalContext
# reminder. It deliberately does NOT hook WebFetch: reminding on every fetched
# page would push agents to save discarded dead-ends, the opposite of the goal
# (register kept sources, not everything touched).
input=$(cat)
# Capture the command value up to its first quote ([^"]*, NOT greedy .*), so the
# match stops inside tool_input and never runs into tool_response.
cmd=$(printf '%s' "$input" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\([^"]*\).*/\1/p' | head -1)
case "$cmd" in
  *"cerne feature approve"*|*"cerne feature complete"*|*"cerne decision new"*|*"cerne decision substituir"*)
    msg="Proveniência (F-0103): você registrou/avançou uma decisão. Se uma fonte externa (artigo, RFC, doc de fornecedor, spec) MOLDOU esta decisão ou design, registre-a agora — sem esperar ser solicitado — com: cerne reference new <título> --url <url> --informs <id>. Registre apenas as fontes que foram úteis (as que moldaram o resultado); becos sem saída e leituras descartadas NÃO viram referência."
    printf '{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"%s"},"suppressOutput":true}\n' "$msg"
    ;;
esac
exit 0
