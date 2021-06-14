---
title: "Country code checking using Cantor Pairing"
description: "A (not so) simple but very efficient way to check country code (~400,000,000 checks per second)."
lead: "Solutions are presented from the easiest to harder for implementation. You can jump directly to section 4 for Cantor Pairing solution."
date: 2021-06-13T12:08:12+07:00
lastmod: 2021-06-13T12:08:12+07:00
draft: false
weight: 50
images: ["country-code-checking-cantor.jpg"]
contributors: ['quy-le']
toc: true
categories: []
tags: ['cantor pairing', 'country code', 'data structure', 'algorithm', 'perfect hash function', 'go', 'golang']
---

### 1. Problem

When I first started working as a developer I used to work with Domain Name System (DNS) and got a task to detect if the [top-level-domain](https://en.wikipedia.org/wiki/List_of_Internet_top-level_domains) of a DNS record is a country-code type or not.  
For example:

```txt
zing.vn => true (Vietnam)
ddc.moph.go.th => true (Thailand)
google.com => false
mozilla.org => false
```
The logic here is to check if the last part after dot of the domain belongs to the list of [ISO 3166-2](https://en.wikipedia.org/wiki/ISO_3166-2) country codes.  
Let's assume we already did other validations and string manipulations to get the last 2 characters (lowercase ASCII, assuming again) from the domain.  
**How can we detect if the 2-character-input is a country code or not?**

```go
// List of 247 country codes
var countryCodes = "ad,ae,af,ag,ai,al,am,an,ao,aq,ar,as,at,au,aw,ax,az,ba,bb,bd,be,bf,bg,bh,bi,bj,bl,bm,bn,bo," +
"br,bs,bt,bv,bw,by,bz,ca,cc,cd,cf,cg,ch,ci,ck,cl,cm,cn,co,cr,cu,cv,cx,cy,cz,de,dj,dk,dm,do,dz,ec,ee,eg,eh,er," +
"es,et,fi,fj,fk,fm,fo,fr,ga,gb,gd,ge,gf,gg,gh,gi,gl,gm,gn,gp,gq,gr,gs,gt,gu,gw,gy,hk,hm,hn,hr,ht,hu,id,ie,il," +
"im,in,io,iq,ir,is,it,je,jm,jo,jp,ke,kg,kh,ki,km,kn,kp,kr,kw,ky,kz,la,lb,lc,li,lk,lr,ls,lt,lu,lv,ly,ma,mc,md," +
"me,mf,mg,mh,mk,ml,mm,mn,mo,mp,mq,mr,ms,mt,mu,mv,mw,mx,my,mz,na,nc,ne,nf,ng,ni,nl,no,np,nr,nu,nz,om,pa,pe,pf," +
"pg,ph,pk,pl,pm,pn,pr,ps,pt,pw,py,qa,re,ro,rs,ru,rw,sa,sb,sc,sd,se,sg,sh,si,sj,sk,sl,sm,sn,so,sr,ss,st,sv,sy," +
"sz,tc,td,tf,tg,th,tj,tk,tl,tm,tn,to,tr,tt,tv,tw,tz,ua,ug,um,us,uy,uz,va,vc,ve,vg,vi,vn,vu,wf,ws,ye,yt,za,zm,zw"

// input is the 2 lowercase ASCII characters (e.g: aa, gg, nz, op, vn, zz)
func IsCountryCode(input string) bool {
  // TODO: Implement logic for this function
}
```

This problem is really simple and I'm sure any decent developer can come up with a solution for it within minutes.  
Let's pause reading a bit and think how are you gonna implement this `IsCountryCode` function?  

`time.Sleep(5*time.Minute)`  

Ok, welcome back.  
I'm going to implement the logic code/test/benchmark in Go, but all approaches should be easy enough to implement on any other programming languages.

### 2. Array solution

The first solution is storing each country code in an array, then inside the `IsCountryCode` function, we compare the `input` string with each item in the array.  

```go
var ccArr []string

func init() {
  // ccArr holds all 247 country codes.
  // => []string{"ad", "ae", ..., "zw"}
	ccArr = strings.Split(countryCodes, ",")
}

// IsCountryCodeByArray checks if the given input matches any item in the ccArr array.
func IsCountryCodeByArray(input string) bool {
	for _, cc := range ccArr {
		if cc == input {
			return true
		}
	}
	return false
}
```

This solution is simple and in fact really fast if the input string is in the couple of first country codes. For example: `IsCountryCodeByArray("ad")` only takes 1 array access, `IsCountryCodeByArray("ae")` takes 2 array access an so on.  
Array access is `O(1)` and really fast, especially if the array can fit into the CPU cache like in this case (more on this in another post).

But this solution is naive because it becomes worse quickly if the input is at the ending part of the `ccArr` array, or in the worst case scenario, not existing in the list. In the worst case scenario, we have to traverse over the whole array in order to return false.  
The benchmark (see test and benchmark code at the end of post) below shows the order of magnitude (150 times) slower in worst case versus happy case.

```shell
# IsCountryCodeByArray("ad") => Hit, best case scenario
# IsCountryCodeByArray("zz") => Miss, worst case scenario

$ go test -run=^$ -bench=BenchmarkCheckCountryCode -cpu 1
BenchmarkCheckCountryCode/___array_naive_hit            192150000              6.40 ns/op
BenchmarkCheckCountryCode/___array_naive_miss             1280067               920 ns/op
```

Algorithm analysis:

- Time complexity: `O(1)`.
  - Best case: Need 1 array (memory) access => O(1).
  - Worst case: Need 247 array access => O(247).
 - Space complexity: `O(1)`.
     - Array has 247 items, each requires 2 bytes (2 ASCII characters) so in total of 494 bytes.  
        Assuming we're not counting space overhead from the string datastructe here (e.g.: each string in Go has at least 8 bytes overhead [^1]).

> ##### Idea
>
> Because the list of country code is alphabetic sorted, you can try to improve this solution by failing-fast while checking the input against the items in `ccArr`.  
> For example: `IsCountryCodeByArray("ah")`.  
> You only need to check until the `ai` item in the `ccArr`, if you reached `ai`, we can be sure that the `ah` input string is not existed in the `ccArr` array.
>
> Question: Try to implement and analyze that solution, how much will it improve on the naive solution and on which case it will be as bad as the naive solution?

### 3. Generic hashmap solution

A better solution is storing the list of country code in a generic (hash)map, then in the `IsCountryCodeByMap` function, we only need to check if the `input` is a key in the map or not.

```go
// ccMap holds 247 country codes as the key.
// We don't need to use map value here, so defines it as struct{} to eliminate overhead.
var ccMap = make(map[string]struct{}, 247)

func init() {
  // Put country codes into the ccMap
  ccs := strings.Split(countryCodes, ",")
  for _, cc := range ccs {
		ccMap[cc] = struct{}{}
	}
}

// IsCountryCodeByMap checks if the given input exists in the ccMap or not.
func IsCountryCodeByMap(input string) bool {
	_, ok := ccMap[input]
	return ok
}
```
Even though this solution is slower than the naive array solution above in (some) happy cases.  
Benchmark result shows a much better result in the worst case scenario.  
The result in both happy and worst case scenario is so consistent which is why 99.99% of the time this is the only solution you'll need in production code.  
Noone has ever been fired by using (hash)map, I'm sure.

```shell
$ go test -run=^$ -bench=BenchmarkCheckCountryCode -cpu 1
BenchmarkCheckCountryCode/____map_string_hit            84334251                13.0 ns/op
BenchmarkCheckCountryCode/____map_string_miss           78706964                13.9 ns/op
```
Algorithm analysis:
- Time complexity: `O(1)`.
  - Same O(1) in both worst and best case scenarios as we need the same 1 hashmap check.
- Space compexity: `O(1)`.
  - Each key need 2 bytes and we have 247 keys so in total 494 bytes same as the naive array solution above.  
    Also assuming that we don't count the overhead from string/hashmap datastructure itself.

> ##### Idea
>
> The type of the map's key matter, try to use a map with integer key instead of string and check if there is any improvement?  
> If our map have more items, what is the advantages of integer map over string and what is the possible downsides?

### 4. Cantor pairing solution

As we said above, most of the time, the hashmap solution above is good enough in real life code.  
So please take these Cantor solutions as the fun experiments when we try to explore outside of the "normal" border.  

Let's recap the strong points from two solutions above, array solution has very fast memory access, while map solution provides stable time complexity in both happy and worst cases.  
How can we combine the advantage of both two solutions into one?

The idea is **somehow we can map each country code into a unique number**, then use that number as the index on the checking array. If we can do so, when checking the country code, we only need 1 array (memory) access for either happy or worst case, which is really fast as shown in the array solution above.  

{{< img src="img/diagram-Mapping.jpg" alt="Mapping string to number" caption="<em>Figure 1: Mapping string to number</em>" class="border-0 text-center" >}}

Experienced readers might realise that what we're trying to do here is re-implementing a hashmap (hash the country code to the integer key of the map).  
The difference is we're trying to design a [perfect hash function](https://en.wikipedia.org/wiki/Perfect_hash_function) (PHF) for our hashmap. PHF is generally easy to design when we have a fixed input set, and in this problem, we know before hand that the input will ranging from `aa` to `zz` so it seems like a perfect place.  

So how we can map each country code into a unique number here? A quick research will lead us to the [Cantor pairing function](https://en.wikipedia.org/wiki/Pairing_function):

> "In mathematics, a pairing function is a process to **uniquely** encode two natural numbers into a single natural number." - Wikipedia

That's exactly what we're looking for. 
Let's implement the Cantor pairing function as our PHF which translates the 2 ASCII characters input to a unique number and use it as the array index.  
Our input is ranging from `aa` to `zz`, which can be translated to the tuple of `[0, 0]` to `[25, 25]`.  We have `cantorPairing("aa") => cantorPairing(0, 0) => 0` and `cantorPairing("zz") => cantorPairing(25, 25) => 1300`, so we will need an array with the size of 1031 to hold all the possible input.

```go
// => Size of the checking array.
var cantorArr = make([]bool, 1301, 1301)

func init() {
  // Mark the index of country code in the cantorArr array
  ccs := strings.Split(countryCodes, ",")
  for _, cc := range ccs {
    idx := cantorPair(cc)
		cantorArr[idx] = true
	}
}

// IsCountryCodeByCantorPairing uses Cantor pairing to calculate the index of the given string
// in the ccArray array.
func IsCountryCodeByCantorPairing(input string) bool {
	idx := cantorPair(input)
	return cantorArr[idx]
}

// cantorPair returns a unique number for the given input using Cantor pairing function.
func cantorPair(input string) uint16 {
  // Reduce unnecessary gap from ASCII [0:97]
  // e.g.: 'a' (97) => (0), 'z' (122) => (25)
  k1 := uint16(input[0] - 97)
	k2 := uint16(input[1] - 97)
	return uint16(((k1+k2)*(k1+k2+1))/2 + k2)
}
```

The benchmark shows using Cantor pairing as the hash function, we can achieve 5 times faster than generic hashmap solution.

```shell
$ go test -run=^$ -bench=BenchmarkCheckCountryCode -cpu 1
BenchmarkCheckCountryCode/cantor_pairing_hit            393816307                2.85 ns/op
BenchmarkCheckCountryCode/cantor_pairing_miss           442224494                2.67 ns/op
```

Algorithm analysis:

- Time complexity: `O(1)`.
  - For both happy and worst case, we only need 1 array access check so it's O(1).
- Space complexity: `O(1)`.
  - We need 1301 boolean items in the array so actual memory usage is 1301 bytes (not countring overhead from array data structure itself).

Why can Cantor pairing solution so much faster than generic hashmap solution when basically both two solutions are hashmap?

- Because generic hashmap uses a generic hash function which works with a much bigger number of keys so it can avoid collision but also makes it much slower compare to our Cantor pairing hash function.
- Cantor pairing hash function here is a PHF, which tailored for this set of data (247 country codes) only. So it's much simpler and don't have to deal with collision.
- Cantor pairing solution operate directly on a small array which can fit directly on CPU cache makes it much faster for CPU to access.  
  While generic hashmap data structure is much more complicated and (may) need pointer reference to the items which cannot leverage CPU cache.

### 5. Cantor pairing with bitmap solution

The Cantor pairing solution above is really fast but has one down side is using more memory, how can we optimize it?

Instead of using 1301 bytes for the checking array, we can operate on bit, so the size required can be reduced by 8 (8 bits makes a byte) to 163 bytes.

{{< img src="img/diagram-CantorBitmap.jpg" alt="Using bitmap instead of byte array" caption="<em>Figure 2: Using bitmap instead of byte array</em>" class="border-0 text-center" >}}

```go
// Max Cantor pair: p(z, z) ~= p(25, 25) ~= 1300 bits ~= 163 bytes
bitMap = make([]byte, 163, 163)

func init() {
  // Mark the index of country code in the cantorArr array bitmap
  ccs := strings.Split(countryCodes, ",")
  for _, cc := range ccs {
    idx := cantorPair(cc)
		bitMap[idx/8] = setBit(bitMap[idx/8], byte(idx%8))
	}
}

// IsCountryCodeByCantorPairing uses Cantor pairing to calculate the index of the given string
// in the bitmap.
func IsCountryCodeByCantorBitmap(input string) bool {
	idx := cantorPair(input)
	return hasBit(bitMap[idx/8], byte(idx%8))
}

// cantorPair returns a unique number for the given input using Cantor pairing function.
func cantorPair(input string) uint16 {
  // Reduce unnecessary gap from ASCII [0:97]
  // e.g.: 'a' (97) => (0), 'z' (122) => (25)
  k1 := uint16(input[0] - 97)
	k2 := uint16(input[1] - 97)
	return uint16(((k1+k2)*(k1+k2+1))/2 + k2)
}

// setBit sets the bit at pos in the byte n.
func setBit(n byte, pos byte) byte {
	n |= (1 << pos)
	return n
}

// hasBit checks the bit at pos in the byte n.
func hasBit(n byte, pos byte) bool {
	return (n & (1 << pos)) > 0
}
```

In order to save up memory, we have to do more calculation to figure out the location of checking bit. Benchmark shows that we are 1.3 times slower than direct access on array index in exchange of 8 times lower in memory, which is a good deal. 

```shell
$ go test -run=^$ -bench=BenchmarkCheckCountryCode -cpu 1
BenchmarkCheckCountryCode/_cantor_bitmap_hit            348379489                3.41 ns/op
BenchmarkCheckCountryCode/_cantor_bitmap_miss           352474602                3.41 ns/op
```

Algorithm analysis:

- Time complexity: `O(1)`.
  - For both happy and worst case, we only need 1 array access check so it's O(1).
- Space complexity: `O(1)`.
  - We need 1301 items in bitmap so so actual memory usage is 1301 bits or 163 bytes (not counting overhead from array data structure itself).

### 6. Conclusion

- Most of the time, generic hashmap is good enough.
- Small array access can be faster than hashmap, while working with small set of data, maybe it's better to use array. CPU cache matter.
- When working with fixed input set, it's good time to think about perfect hash function.
- Test, benchmark and profiling is valuable when optimizing.
- Micro-benchmarking is overkill most of the time and premature optimization is the root of all evils [^2].
- This Cantor pairing PHF can become bloat quickly, e.g. `cantorPairing(500, 500) = 501000`, so it cannot be used as a generic hash function.  
  What you should take away from this post is the way of solving specific problem only.

Source code:

- [main.go](code/main.go): Implementation for all solutions.
- [main_test.go](code/main_test.go): Test and benchmark.

Or see it on [Github](https://github.com/lnquy/www/tree/main/write/content/blog/country-code-checking-cantor/code).

```shell
$ go test -run=^$ -bench=BenchmarkCheckCountryCode -cpu 1
BenchmarkCheckCountryCode/___array_naive_hit            234570860                5.15 ns/op
BenchmarkCheckCountryCode/____map_string_hit            73966557                15.3 ns/op
BenchmarkCheckCountryCode/cantor_pairing_hit            423420466                2.81 ns/op
BenchmarkCheckCountryCode/_cantor_bitmap_hit            305096086                4.03 ns/op
BenchmarkCheckCountryCode/___array_naive_miss            1411725               849 ns/op
BenchmarkCheckCountryCode/____map_string_miss           72614508                15.1 ns/op
BenchmarkCheckCountryCode/cantor_pairing_miss           447370311                2.70 ns/op
BenchmarkCheckCountryCode/_cantor_bitmap_miss           323894680                3.72 ns/op
```



[^1]:https://dlintw.github.io/gobyexample/public/memory-and-sizeof.html

[^2]: http://wiki.c2.com/?PrematureOptimization