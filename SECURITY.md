# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 2.1.x   | :white_check_mark: |
| 2.0.x   | :white_check_mark: |
| < 2.0   | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly.

### How to Report

1. **Do NOT** create a public GitHub issue for security vulnerabilities
2. Send an email to the maintainer with details of the vulnerability
3. Include the following information:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

### What to Expect

- **Acknowledgment**: You will receive an acknowledgment within 48 hours
- **Updates**: We will keep you informed about the progress of fixing the vulnerability
- **Resolution**: We aim to resolve critical vulnerabilities within 7 days
- **Credit**: With your permission, we will credit you in the release notes

## Security Best Practices

When deploying this API, please consider the following security recommendations:

### Network Security

- Deploy behind a reverse proxy (nginx, Traefik, etc.)
- Use HTTPS/TLS for all production traffic
- Implement rate limiting to prevent abuse
- Restrict access to trusted networks/IPs if possible

### Container Security

- Run the container as a non-root user
- Use read-only file systems where possible
- Keep the base image updated
- Scan images for vulnerabilities regularly

### API Security

- The API does not include authentication by default
- Implement authentication/authorization for production use
- Consider adding API keys or OAuth2 for access control
- Monitor and log all API requests

### iperf3 Testing Considerations

- Be aware that bandwidth tests can consume significant network resources
- Implement bandwidth limits (default: 100 Mbit/s) to prevent abuse
- Consider restricting which target servers can be tested
- Monitor for unusual testing patterns

## Known Security Considerations

1. **No Built-in Authentication**: The API is designed for internal/trusted network use. Add authentication for public deployments.

2. **Resource Consumption**: Bandwidth tests can be resource-intensive. The default 100 Mbit/s limit helps prevent abuse.

3. **Target Server Access**: The API can connect to any iperf3 server. Consider implementing an allowlist for production use.

## Security Updates

Security updates will be released as patch versions (e.g., 2.1.1) and announced in:
- GitHub Releases
- CHANGELOG in README.md

We recommend always running the latest version.
