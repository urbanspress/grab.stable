#!/usr/bin/env python3
"""
Uploads release assets (visual PDF guides + cross-platform binaries)
to GitHub releases across both development (origin) and production (stable) repositories.
"""

import os
import sys
import subprocess
import time

REPOS = [
    "urbanspress/grab.progress",
    "urbanspress/grab.stable"
]

MILESTONES = [
    "v0.1.0",
    "v0.2.0",
    "v0.3.0",
    "v0.4.0",
    "v0.5.0",
    "v0.6.0",
    "v0.7.0",
    "v0.8.0",
    "v0.9.0",
    "v1.0.0",
    "v1.0.1",
    "v1.1.0"
]

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
PROJECT_ROOT = os.path.abspath(os.path.join(BASE_DIR, "..", ".."))
PDF_DIR = os.path.join(BASE_DIR, "pdf")
DIST_DIR = os.path.join(PROJECT_ROOT, "dist")

def upload_file(repo, tag, filepaths):
    cmd = ["gh", "release", "upload", tag] + filepaths + ["--clobber", "--repo", repo]
    print(f"Uploading to {repo} [{tag}]: {[os.path.basename(f) for f in filepaths]}...")
    max_retries = 3
    for attempt in range(max_retries):
        res = subprocess.run(cmd, capture_output=True, text=True)
        if res.returncode == 0:
            print(f"  -> SUCCESS")
            return True
        else:
            print(f"  -> Warning (attempt {attempt+1}/{max_retries}): {res.stderr.strip()}")
            time.sleep(2)
    print(f"  -> FAILED after {max_retries} attempts.")
    return False

def main():
    failed = False
    for tag in MILESTONES:
        pdf_file = os.path.join(PDF_DIR, f"GRAB-{tag}-Guide.pdf")
        if not os.path.exists(pdf_file):
            print(f"Error: {pdf_file} does not exist!")
            sys.exit(1)
            
        files_to_upload = [pdf_file]
        if tag in ("v1.0.0", "v1.0.1", "v1.1.0"):
            win_bin = os.path.join(DIST_DIR, "grab-windows-amd64.exe")
            linux_bin = os.path.join(DIST_DIR, "grab-linux-amd64")
            if not os.path.exists(win_bin) or not os.path.exists(linux_bin):
                print(f"Error: Binaries in {DIST_DIR} missing!")
                sys.exit(1)
            files_to_upload.extend([win_bin, linux_bin])
            sums_file = os.path.join(DIST_DIR, "SHA256SUMS.txt")
            if os.path.exists(sums_file):
                files_to_upload.append(sums_file)
            
        for repo in REPOS:
            success = upload_file(repo, tag, files_to_upload)
            if not success:
                failed = True
                
    if failed:
        print("\nSome uploads failed. Please review errors above.")
        sys.exit(1)
    else:
        print("\nAll assets uploaded successfully to both repositories!")

if __name__ == "__main__":
    main()
