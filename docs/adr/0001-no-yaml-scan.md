# Do not scan rendered YAML

SafeLane verifies GitHub and GHCR, renders the trusted bundle, and continues. It does not send that YAML to an external infrastructure checker. That path was evaluated and dropped; it is not part of the product.
