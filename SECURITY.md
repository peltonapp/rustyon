# Security Policy

## Supported versions

Security fixes are released for the latest minor version of rustyon.

## Reporting a vulnerability

Please do not open a public issue for security problems.

Report vulnerabilities through GitHub's private reporting: go to the [Security tab](https://github.com/peltonapp/rustyon-go/security) and choose **Report a vulnerability**. Include the input that triggers the problem, the rustyon version and what you expected.

You should get a first response within a week. Once a fix is ready we publish a release and a GitHub security advisory crediting the reporter, unless you prefer to stay anonymous.

## Scope

rustyon parses untrusted input, so the following are in scope:

- panics, crashes or unbounded memory or CPU use while parsing, decoding or formatting
- stack exhaustion through deeply nested input (nesting is limited to 1000 levels)
- incorrect output that could change the meaning of a document after formatting
