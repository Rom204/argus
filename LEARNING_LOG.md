# Learning Log & Insights - Argus Project (Mini-EDR)

## Phase 1: Infrastructure and Development Environment (August 2026)

### What I Did
* Set up a virtual machine (Ubuntu Linux) on macOS using UTM.
* Configured Remote Development using the SSH extension in VS Code.
* Installed development tools, Git, and AI assistant (Claude) within the remote server.

### New Concepts Learned
* **Virtualization and Hypervisor:** Running a complete operating system (Guest) with its own kernel on top of a host operating system (Host).
* **Remote-SSH:** A workflow where the local code editor (VS Code) acts merely as a user interface (UI), while all files, compilations, and terminal commands physically take place on the remote Linux server. (must make sure we open the door from inside the remote VM for the ssh server before trying to open with the extension).
* **CO-RE (Compile Once - Run Everywhere):** A crucial concept in the eBPF ecosystem that allows compiling the code once and running it across different host kernel versions without needing to recompile.

### Advanced OS Concepts Encountered (Installation Phase)
* **File Locks vs. Mutexes in Action:** Encountered an `apt` lock issue (`Could not get lock /var/lib/dpkg/lock-frontend`). This is a real-world application of mutual exclusion. While a Mutex protects critical sections in RAM to prevent thread race conditions, a file lock (using system calls like `flock`) protects critical sections on the disk (the package database) from concurrent processes, preventing data corruption.
* **Minimal OS Distributions:** Discovered that VM-oriented OS distributions often ship without non-essential background services (e.g., `systemd-timesyncd` for automatic time syncing) to conserve host resources. This is a standard DevOps practice for lightweight environments.
* **Time Synchronization Issues:** Learned that an out-of-sync VM clock causes package managers (`apt`) to fail validation checks, as downloaded package signatures appear to originate from the "future". Fixed by manually syncing the system clock using the `date -s` command.

### Aha Moments & Insights
* **Separation of Environments:** Linux and Ubuntu are not synonymous. Linux is just the engine (the kernel), while Ubuntu is the complete distribution (the operating system). eBPF specifically requires the Linux kernel to operate.
* **File Location:** When writing code via Remote-SSH, the project files are not saved on the Mac. They reside on the Ubuntu virtual disk. The Mac simply functions as a "remote control" and display.
* **Remote Git Execution:** Because the codebase sits on the Linux VM, Git commands (`clone`, `commit`, `push`) must be executed from the remote Linux terminal, not the local Mac terminal.

### Errors Encountered & Solutions
* **Error:** VS Code refused to connect to the Ubuntu IP (`ssh child died`).
  * **Solution:** Standard Ubuntu Desktop installations do not include an active SSH server by default. Fixed by installing `openssh-server` and enabling the service.
* **Error:** A security prompt in the VS Code terminal asked for `(yes/no)` but didn't display my keystrokes.
  * **Solution:** Realized that secure terminal fields hide typing. Blindly typing `yes` and hitting Enter successfully passed the SSH fingerprint verification and prompted for the password.
* **Error:** Ran `git init` and suddenly all hidden system files (like `.bashrc`) appeared in the source control tab.
  * **Solution:** Accidentally initialized Git in the root home directory (`/home/rom/`) instead of a dedicated project folder. Fixed by deleting the hidden `.git` folder (`rm -rf .git`) and correctly cloning the repository into the `/argus` directory.