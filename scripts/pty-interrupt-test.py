import argparse
import sys
import os
import time
import pty
import select
import shlex
import re

def clean_ansi(text):
    return re.sub(r'\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])', '', text)

def main():
    parser = argparse.ArgumentParser(description="Live OpenCode PTY Test")
    parser.add_argument("--repo", required=True)
    parser.add_argument("--cmd", default="opencode")
    parser.add_argument("--prompt", default="Run the shell command sleep 20 and do not do anything else.")
    parser.add_argument("--paste-mode", choices=["raw", "bracketed"], default="bracketed")
    parser.add_argument("--timeout", type=int, default=60)
    parser.add_argument("--auto-approve-permissions", action="store_true")
    parser.add_argument("--auto-approve-sleep", action="store_true")
    parser.add_argument("--dry-run-self-test", action="store_true")
    args = parser.parse_args()

    if args.dry_run_self_test:
        print("Dry run passed.")
        sys.exit(0)

    if os.getenv("OPENCODE_LIVE_PTY_TEST") != "1":
        print("Refusing: OPENCODE_LIVE_PTY_TEST=1 required.")
        sys.exit(1)

    log_file = f"/tmp/opencode/pty-interrupt-{int(time.time())}.log"
    os.makedirs("/tmp/opencode", exist_ok=True)
    f_log = open(log_file, "w")

    pid, master_fd = pty.fork()
    if pid == 0:
        os.chdir(args.repo)
        os.environ["TERM"] = "xterm-256color"
        os.environ["COLUMNS"] = "80"
        os.environ["LINES"] = "24"
        cmd_args = shlex.split(args.cmd)
        os.execvp(cmd_args[0], cmd_args)

    start_time = time.time()
    transcript = ""
    prompt_sent = False
    prompt_sent_time = 0
    interrupt_sent = False
    prompt_retry_sent = False
    post_prompt_text = ""

    def send_prompt(fd, p, mode):
        if mode == "bracketed":
            # \x1b[200~ {prompt} \x1b[201~ \r
            payload = f"\x1b[200~{p}\x1b[201~\r".encode()
        else:
            payload = (p + "\r").encode()
        os.write(fd, payload)

    try:
        while time.time() - start_time < args.timeout:
            rfds, _, _ = select.select([master_fd], [], [], 0.1)
            if master_fd in rfds:
                data = os.read(master_fd, 4096).decode(errors='ignore')
                transcript += data
                f_log.write(data)
                f_log.flush()
                
                clean = clean_ansi(transcript)
                
                if not prompt_sent:
                    if "Ask anything" in clean:
                        send_prompt(master_fd, args.prompt, args.paste_mode)
                        prompt_sent = True
                        prompt_sent_time = time.time()
                        print(f"Prompt sent ({args.paste_mode}).")
                else:
                    post_prompt_text += clean_ansi(data)
                    running_cue = any(x in clean_ansi(data).lower() for x in ["thought", "assistant", "tool"])
                    if not running_cue and not prompt_retry_sent and time.time() - prompt_sent_time > 10:
                        print("No running cue detected after 10s. Retrying with raw CR.")
                        send_prompt(master_fd, args.prompt, "raw")
                        prompt_retry_sent = True

                    if not interrupt_sent:
                        if "Permission required" in post_prompt_text and ("Allow once" in post_prompt_text or "Allow always" in post_prompt_text) and "Reject" in post_prompt_text:
                            if args.auto_approve_sleep and "sleep 20" in clean and not re.search(r"sleep 20\s*[;&|>]", clean):
                                print("Auto-approving safe sleep.")
                                # Assuming standard UI interaction: press Enter for Allow (default)
                                os.write(master_fd, b"\r")
                                post_prompt_text = "" # Reset to avoid double Enter
                            else:
                                print("Error: Permission UI detected (unsupported/unsafe).")
                                sys.exit(1)
                        if re.search(r"esc.{0,20}interrupt", post_prompt_text.lower()):
                            print("Success: Observed 'esc interrupt'.")
                            sys.exit(0)
        
        print(f"Timeout reached. Transcript logged to {log_file}")
        sys.exit(1)
    finally:
        # Cleanup
        try:
            os.write(master_fd, b"\x03") # Ctrl-C
            os.write(master_fd, b"\x1b") # ESC
            import signal
            os.kill(pid, signal.SIGTERM)
            time.sleep(0.5)
            os.kill(pid, signal.SIGKILL)
        except:
            pass
        f_log.close()
        os.close(master_fd)

if __name__ == "__main__":
    main()
