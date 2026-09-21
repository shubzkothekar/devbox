# DevBox Workspace Directory

This directory is the root mount point for your project repositories.

## Usage
Clone or place your project repositories directly inside this folder:
```bash
workspace/
├── my-backend-api/
├── my-frontend-web/
└── my-microservice/
```

Inside the DevBox container, this directory is mounted at `/workspace`.
When you connect via SSH or open in your editor (Zed or VS Code), your terminal lands directly in `/workspace`.
