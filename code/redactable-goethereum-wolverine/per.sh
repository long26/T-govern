#!/bin/bash
# per.sh - 授权脚本

find . -name "*.sh" -exec chmod +x {} \;
chmod +x cmd/ethkey/ethkey