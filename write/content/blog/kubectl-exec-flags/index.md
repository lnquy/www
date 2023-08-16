---
title: "Kubectl Exec Flags"
description: "What exactly does the -t and -i flags in kubectl exec means?"
lead: "What exactly does the -t and -i flags in kubectl exec means?"
date: 2021-08-15T21:41:47+07:00
lastmod: 2021-08-15T21:41:47+07:00
draft: false
toc: false
weight: 50
images: []
contributors: ['quy-le']
categories: []
tags: ['k8s', 'kubernetes', 'kubectl', 'cli', 'flag', 'tty', 'file descriptor']
---

```shell
$ kubectl help exec
Execute a command in a container.

Examples:
  # Switch to raw terminal mode, sends stdin to 'bash' in ruby-container from pod mypod
  # and sends stdout/stderr from 'bash' back to the client
  kubectl exec mypod -c ruby-container -i -t -- bash -il
 
  # Get output from running 'date' command from the first pod of the deployment mydeployment, using the first container
by default
  kubectl exec deploy/mydeployment -- date
  
Options:
  -i, --stdin=false: Pass stdin to the container
  -t, --tty=false: Stdin is a TTY
```

A program works by this flow: `[Accept input data] => [Process] => [Return]`. 
When a Unix program started up, by default there will be 3 file descriptors ("channels") attaches to the process (stdin, stdout and stderr, `fd=0,1,2`). 

- The process can choose to listen on the stdin for input data (e.g.: sh/bash/vi command listens on stdin for user input-ed commands) or not (e.g.: date/ls/cd commands).
- And also, process can print the result into stdout/stderr (e.g.: ls/pwd command) or print nothing (e.g.: cd command).

For commands (which will be executed inside the container) that DON'T NEED to accept any input from user's terminal, we can execute it directly without `-i -t` flags as below. 
Exec command simply execute the command inside the container and print the result (if exists) back to the user's terminal.

```shell
$ kc exec nettools-5-2869s date   
kubectl exec [POD] [COMMAND] is DEPRECATED and will be removed in a future version. Use kubectl exec [POD] -- [COMMAND] instead.
Sun Jun  6 07:07:44 UTC 2021

$ kc exec nettools-5-2869s -- date
Sun Jun  6 07:07:52 UTC 2021

# => With or without 2 dashes (--) is the same, but with dashes is preferred, especially if the input command has arguments.

$ kc exec nettools-5-2869s -- sleep 1
# Exec command ends after 1s without printing anything else to the terminal (sleep command doesn't write anything to stdout/stderr).
```

For commands (which will be executed inside the container) that NEED to accept any input from user's terminal, we have to provide `-i ` flags for exec command.

- `-i` means passing user's input from the terminal into the stdin of the command being executed in the container. 
  Also, the stdout/stderr of the executing command (inside container) will be forwarded back to the user's terminal (stdout/stderr respectively).
  E.g.: `kc exec pod1 -i -- sh ` 
  => Pass anything user has typed (on their terminal stdin) into the stdin of `sh` command inside the container. So the sh command can "see" what user has typed and execute those.
  Also, the result from `sh` will be forwarded back and printed on user's terminal now.

```shell
$ kc exec nettools-5-2869s -i -- sh
pwd   # Command I typed in the terminal being passed into the sh's stdin
/home/nuser
cd /   # Command I typed in the terminal being passed into the sh's stdin
ls   # Command I typed in the terminal being passed into the sh's stdin
bin
# [...]
usr
var
whoami   # Command I typed in the terminal being passed into the sh's stdin
whoami: unknown uid 1000510000
exit   # Command I typed in the terminal being passed into the sh's stdin
command terminated with exit code 1
```

- `-i` is enough to interact with the command being executed inside the container but to get more from it, we need to init/upgrade to TTY session too (still looking for what is the benefits here though), so `-t` flags is also needed.
  Note: 

  - I don't have pods with TTY enabled on k8s so switching to Docker here.
  - Actually it's PTS here, not TTY as we will see below: https://www.golinuxcloud.com/difference-between-pty-vs-tty-vs-pts-linux/#PTS

  ```shell
  # Case 1: Run container without -t option enabled.
  $ docker run --rm --name nt -d lnquy/nettools:0.0.7
  $ docker exec nt tty
  not a tty   # Exec without -t => inside container is not a TTY
  $ docker exec -t nt tty
  /dev/pts/0  # Exec with -t => inside container is a TTY (PTS) now
  
  # Case 2: Run container with -t option enabled.
  $ docker run --rm --name nt -d -t lnquy/nettools:0.0.7
  $ docker exec nt tty
  not a tty
  $ docker exec -t nt tty
  /dev/pts/1  # TTY with diferent id now, /dev/pts/0 was reserved from docker run -t, I guess
  $  docker exec nt ls /dev/pts
  0
  ptmx
  # We don't see 1 because the session ended after tty command executed and exited on line #12
  
  
  # If we use -t alone in exec, the TTY still can be created, but we canot sending any data into the container stdin
  # which makes the TTY session useless.
  # => Use combined -it or not using it at all.
  $ docker exec -t nt sh
  ~ $   # Show TTY established, but cannot input anything.
  $ docker exec -t nt bash
  bash-4.4$   # Same as above
  ```

When using combined `-it`, it means establishing a TTY session to the container (`-t`) and also link the container stdin/stdout/stderr with user's terminal stdin/stdout/stderr respectively (`-i`).

  - When user input anything on their terminal's stdin, these data will be forwarded into container's stdin.
  - The command running inside container accept those data, process/execute it and returns the result back in container's stdout/stderror which will be forwarded back to user terminal's stdout/stderr.

=> Use combined `-it` or not using it at all.

```shell
$ docker exec -it nt bash
bash-4.4$ pwd
/home/nuser
bash-4.4$ cd /
bash-4.4$ ls
bin    dev    etc    home   lib    media  mnt    proc   root   run    sbin   srv    sys    tmp    usr    var
bash-4.4$ whoami
nuser
bash-4.4$ exit
exit
```


