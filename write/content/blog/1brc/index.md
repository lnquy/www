---
title: "The One Billion Row Challenge"
description: "How to calculate 1 billion row as fast as possible. Or the journey to optimize Go solution to be 40x faster."
lead: "How did I optimize a Go solution to be 40 times faster comparing to the baseline solution in the One Billion Row Challenge (1BRC)."
date: 2024-01-10T19:17:00+07:00
lastmod: 2024-01-10T19:17:00+07:00
draft: true
weight: 50
images: []
contributors: ["quy-le"]
toc: true
categories: []
tags: ['go', 'golang', 'optimization', 'profiling', 'performance']
---

> tl;dr: The final Go solution took ~6.5s to finish calculating 1 billion row of temperature measurements. 40 times faster than the baseline Go solution and 2.5 times faster than the fastest Java solution at the time.  
> Tested on my Macbook M1 Pro, 8 cores (6 performance and 2 efficiency), 16GB RAM. Measurement data file is 13.8GB.

## 1. The challenge
Recently, this [1BRC repository](https://github.com/gunnarmorling/1brc) made it way to the Github trending repos and caught my attenttion.  
Simply put, given a flat file with 1 billion lines, each line consist of a weather station and its temperature measurement (e.g.: `Dushanbe;10.4`). How fast can you calculate the minimum, average and maximum temperature for all the stations?  
Even though the challenge specifically designed for Java and doesn't accept submission from the other programming languages, I found its technical challenge quite interesting and want to try the implementation in Go.

#### Preparation
Since we need to have the measurement data file and know the execution time of the baseline solution first, let's clone the 1BRC repo and prepreare the data.
```bash
$ git clone https://github.com/gunnarmorling/1brc && cd $_

# Make sure you have Java SDK installed on your machine 
$ ./mvnw clean verify   # Build Java codes
$ ./create_measurements.sh 1000000000   # Create the measurements.txt file with 1 billion rows
$ ls -l | grep measurements.txt
-rw-r--r--   1 quyle  staff  13795430841 Jan  4 14:18 measurements.txt
```
1 billion measurements data file is 13.8GB.

#### Baseline and target
Let's measure the Java baseline solution:
```bash
$ ./calculate_average_baseline.sh
real   3m6.814s
user   3m1.922s
sys    0m5.308s
```
So Java baseline solution took 3m6s to finished the solution.
Let's see how well the fastest Java solution perform on my machine.
At the time writing this post, [royvanjin's solution](https://github.com/gunnarmorling/1brc/blob/7a617720ad20ce4b22bc2d03c7387b3f33fc4803/src/main/java/dev/morling/onebrc/CalculateAverage_royvanrijn.java) on `21.0.1-graal` JDK was the fastest one on the leaderboard.
```bash
# Make sure to install sdkman first as some Java solution depends on a specific SDK version / features.
# https://sdkman.io/install
$ ./calculate_average_royvanrijn.sh
real   0m17.082s
user   0m17.630s
sys    0m7.800s
```
So the fastest Java solution took 17s to finish the challenge on my machine.
Let's set the target for Go solution to be at least as fast as the Java solution

#### Further analysis
This challenge can be divided into 2 sub-problems:
1. How to read the input file from disk to memory as fast as possible? (`p#1`)  
    => I/O bound, this depends on how fast we can (sequentially) read from the disk and how much memory we have.  
    => Since the file size (13.8GB) is smaller than the Mac machine RAM (16GB), we don't have to worry about the memory and only need to care about disk read speed here.  
2. How to calculate the data available on the memory as fast as possible? (`p#2`)  
    => CPU bound.  

So the optimal execution time for this challenge is the `min(execTime(p#1), execTime(p#2))`.  
If we can concurrently execute `p#1` and `p#2`, then the fastest execution time archived when `execTime(p#1) == execTime(p#2)`.

It's hard to measure CPU limit, but at least we can assume this is the I/O bound challenge and focus on measurement for `p#1`.
```bash
# Test disk write speed
$ dd if=/dev/zero bs=2048k of=tstfile count=1024
2147483648 bytes transferred in 0.979968 secs (2191381400 bytes/sec)
# Test disk read speed
$ dd if=tstfile bs=2048k of=/dev/null count=1024
2147483648 bytes transferred in 0.343498 secs (6251808302 bytes/sec)
```

My Mac machine can sequently read 6.25GB/s from the disk, so the fastest time to read the full measurement file from disk to memory is `13.8/6.25 = 2.2s`.
So 2.2s is the optimal target we can archive on my Mac machine, assumming we have enough CPU resources.

**Insights:**
1. This is an I/O bound and possibly CPU bound challenge.
2. Optimize solution for time rather than space, don't have to care about how much memory is used.
3. Target execution time: <17s
4. Optimal target execution time: 2.2s

## 2. Go solutions
### 2.1. Baseline
...

## 3. Further enhancements
...
