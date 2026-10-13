#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go_cmd=${GO:-go}

# Use go list, rather than searching source text: include transitive imports.
for module in platform commerce identity; do
  dependencies=$(cd "modules/$module" && GOWORK=off "$go_cmd" list -deps -f '{{.ImportPath}}' ./...)
  if printf '%s\n' "$dependencies" | rg -q '^example.com/ecommerce/(api|platform|commerce|identity)(/|$)' ; then
    forbidden=$(printf '%s\n' "$dependencies" | rg '^example.com/ecommerce/(api|platform|commerce|identity)(/|$)' | rg -v "^example.com/ecommerce/$module(/|$)" || true)
    if [[ -n "$forbidden" ]]; then printf 'Forbidden dependency in %s:\n%s\n' "$module" "$forbidden"; exit 1; fi
  fi
  if printf '%s\n' "$dependencies" | rg -q '^github.com/gin-gonic/gin(/|$)'; then
    printf '%s must not depend on Gin\n' "$module"; exit 1
  fi
done
# Pure domain rules must not import applications, ports, or concrete adapters.
domain_dependencies=$(cd modules/commerce && GOWORK=off "$go_cmd" list -deps -f '{{.ImportPath}}' ./domain/...)
if printf '%s\n' "$domain_dependencies" | rg -q '^example.com/ecommerce/commerce/(application|ports|adapters)(/|$)'; then
  echo 'Domain must not import outer layers'; exit 1
fi
application_dependencies=$(cd modules/commerce && GOWORK=off "$go_cmd" list -deps -f '{{.ImportPath}}' ./application/...)
if printf '%s\n' "$application_dependencies" | rg -q '^example.com/ecommerce/commerce/adapters(/|$)'; then
  echo 'Applications must depend on ports, not concrete adapters'; exit 1
fi
identity_dependencies=$(cd modules/identity && GOWORK=off "$go_cmd" list -deps -f '{{.ImportPath}}' ./application/... ./domain/... ./ports/...)
if printf '%s\n' "$identity_dependencies" | rg -q '^example.com/ecommerce/identity/adapters(/|$)'; then
  echo 'Identity core must not import concrete adapters'; exit 1
fi
echo 'Module and domain boundaries OK'
