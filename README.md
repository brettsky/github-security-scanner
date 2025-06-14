# GitHub Security Scanner

A tool to scan GitHub repositories for potential security issues and exposed secrets.

## Features

- Scans public GitHub repositories for potential security issues
- Detects exposed secrets, credentials, and sensitive files
- Configurable search patterns and file types
- Rate limit aware with automatic backoff
- Detailed statistics and reporting

## Setup

1. Clone the repository:
```bash
git clone https://github.com/yourusername/github-security-scanner.git
cd github-security-scanner
```

2. Create your configuration file:
```bash
cp config.template.json config.json
```

3. Get a GitHub Personal Access Token:
   - Go to GitHub Settings → Developer Settings → Personal Access Tokens
   - Click "Generate new token (classic)"
   - Give it a name (e.g., "GitHubScanner")
   - Select scopes: `repo` and `read:packages`
   - Copy the token

4. Edit `config.json` and replace `YOUR_GITHUB_TOKEN_HERE` with your token

## Usage

Run the scanner:
```bash
go run main.go -config config.json -output json
```

Options:
- `-config`: Path to configuration file (default: config.json)
- `-output`: Output format (json or csv, default: json)

## Output

The scanner will create a `findings.json` or `findings.csv` file with the results, including:
- Repository name
- File path
- URL to the file
- Pattern matched
- Severity level

## Security Note

Never commit your `config.json` file or any files containing your GitHub token. The repository includes a `.gitignore` file to prevent accidental commits of sensitive data.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the LICENSE file for details. 