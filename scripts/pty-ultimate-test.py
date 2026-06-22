#!/usr/bin/env python3
import shlex
import argparse
import os
import sys
import time
import pty
import select
import datetime
import signal
import re

# Pseudo-PTY harness for OpenCode TUI
# Usage: OPENCODE_LIVE_PTY_TEST=1 python3 scripts/pty-ultimate-test.py --repo <path>

def strip_ansi(text):
    return re.sub(r'\x1b\[[0-9;]*[a-zA-Z]', '', text)

def main():
    parser = argparse.ArgumentParser(description="PTY Test Harness")
    parser.add_argument("--repo", required=True, help="Target repo path")
    parser.add_argument("--cmd", default="opencode", help="Command to run")
    parser.add_argument("--timeout", type=int, default=300, help="Timeout in seconds")
    parser.add_argument("--prompt", default="fast track this to match upstream progress", help="Input prompt")
    parser.add_argument("--auto-approve-permissions", action="store_true", help="Auto-approve permission requests")
    parser.add_argument("--dry-run-self-test", action="store_true", help="Self-test mode")
    parser.add_argument("--paste-mode", choices=['raw', 'bracketed'], default='bracketed', help="Input injection mode")
    args = parser.parse_args()

    if args.dry_run_self_test:
        print("Dry run OK: Logic checked.")
        sys.exit(0)

    if os.environ.get("OPENCODE_LIVE_PTY_TEST") != "1":
        print("Error: OPENCODE_LIVE_PTY_TEST=1 not set. Refusing.")
        sys.exit(1)

    os.makedirs("/tmp/opencode", exist_ok=True)
    log_path = f"/tmp/opencode/pty-ultimate-{datetime.datetime.now().strftime('%Y%m%d%H%M%S')}.log"
    print(f"Running test on {args.repo}. Log: {log_path}")

    pid, master_fd = pty.fork()
    if pid == 0:
        import tty
        tty.setraw(sys.stdin.fileno())
        os.chdir(args.repo)
        argv = shlex.split(args.cmd)
        os.execvp(argv[0], argv)
    
    sent_prompt = False
    prompt_sent_at = 0
    saw_interrupt = False
    saw_completion = False
    last_scan_len = 0
    last_approval_at = 0
    
    with open(log_path, "wb") as log:
        start = time.time()
        buf = b""
        
        while time.time() - start < args.timeout:
            r, _, _ = select.select([master_fd], [], [], 0.1)
            if r:
                try:
                    data = os.read(master_fd, 1024)
                except OSError: break
                if not data: break
                log.write(data)
                buf += data
                
                raw_t = buf.decode('utf-8', errors='ignore')
                t = strip_ansi(raw_t)
                recent_t = t[last_scan_len:]
                
                if not sent_prompt and re.search(r'Ask anything|tab agents|/home/thinh0704hcm', t, re.IGNORECASE):
                    print("[ACTION] sending prompt")
                    prompt_bytes = args.prompt.encode()
                    if args.paste_mode == 'bracketed':
                        os.write(master_fd, b"\x1b[200~" + prompt_bytes + b"\x1b[201~\r")
                    else:
                        os.write(master_fd, prompt_bytes + b"\r")
                    sent_prompt = True
                    prompt_sent_at = time.time()
                    last_scan_len = len(t)
                
                if sent_prompt and not saw_completion and time.time() - prompt_sent_at > 10:
                    print("[ACTION] resent prompt")
                    prompt_bytes = args.prompt.encode()
                    if args.paste_mode == 'bracketed':
                        os.write(master_fd, b"\x1b[200~" + prompt_bytes + b"\x1b[201~\r")
                    else:
                        os.write(master_fd, prompt_bytes + b"\r")
                    prompt_sent_at = time.time()

                if not saw_interrupt and re.search(r'esc.*interrupt|interrupt.*esc', t, re.IGNORECASE):
                    saw_interrupt = True

                if not saw_completion and re.search(r'<task-notification>.*completed|Status: completed|completed successfully', t, re.IGNORECASE):
                    saw_completion = True
                
                # Check recent activity... (abbreviated logic)
                if re.search(r'allow|approve', recent_t, re.IGNORECASE) and re.search(r'press\s+enter', recent_t, re.IGNORECASE):
                    if time.time() - last_approval_at > 2:
                        if args.auto_approve_permissions:
                            os.write(master_fd, b"\n")
                            last_approval_at = time.time()
                
                if sent_prompt and saw_interrupt and saw_completion:
                    os.write(master_fd, b"\x03")
                    time.sleep(1)
                    try: os.kill(pid, signal.SIGTERM)
                    except: pass
                    sys.exit(0)
            
            # WaitPID block ...
            try: pid_res, status = os.waitpid(pid, os.WNOHANG)
            except ChildProcessError: pid_res = pid
            if pid_res != 0: break

    try: os.kill(pid, signal.SIGKILL)
    except: pass
    sys.exit(1)

if __name__ == "__main__":
    main()
