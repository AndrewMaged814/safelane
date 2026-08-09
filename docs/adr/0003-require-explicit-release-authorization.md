# Require a valid rollout decision for release

SafeLane rejects release when `decision.json` is missing, malformed, stale, identity-mismatched, or
unapproved. Falling back to a careful profile would be operationally conservative but would silently
bypass the authorization boundary; a Strict fallback may therefore be shown only as a diagnostic
preview and is never rendered or applied as permission to deploy.
