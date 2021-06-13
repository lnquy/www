// Advanced Cantor Pair Mapping
package main

import (
	"math"
	"strings"
)

var (
	countryCodes = "ad,ae,af,ag,ai,al,am,an,ao,aq,ar,as,at,au,aw,ax,az,ba,bb,bd,be,bf,bg,bh,bi,bj,bl,bm,bn,bo," +
		"br,bs,bt,bv,bw,by,bz,ca,cc,cd,cf,cg,ch,ci,ck,cl,cm,cn,co,cr,cu,cv,cx,cy,cz,de,dj,dk,dm,do,dz,ec,ee,eg,eh,er," +
		"es,et,fi,fj,fk,fm,fo,fr,ga,gb,gd,ge,gf,gg,gh,gi,gl,gm,gn,gp,gq,gr,gs,gt,gu,gw,gy,hk,hm,hn,hr,ht,hu,id,ie,il," +
		"im,in,io,iq,ir,is,it,je,jm,jo,jp,ke,kg,kh,ki,km,kn,kp,kr,kw,ky,kz,la,lb,lc,li,lk,lr,ls,lt,lu,lv,ly,ma,mc,md," +
		"me,mf,mg,mh,mk,ml,mm,mn,mo,mp,mq,mr,ms,mt,mu,mv,mw,mx,my,mz,na,nc,ne,nf,ng,ni,nl,no,np,nr,nu,nz,om,pa,pe,pf," +
		"pg,ph,pk,pl,pm,pn,pr,ps,pt,pw,py,qa,re,ro,rs,ru,rw,sa,sb,sc,sd,se,sg,sh,si,sj,sk,sl,sm,sn,so,sr,ss,st,sv,sy," +
		"sz,tc,td,tf,tg,th,tj,tk,tl,tm,tn,to,tr,tt,tv,tw,tz,ua,ug,um,us,uy,uz,va,vc,ve,vg,vi,vn,vu,wf,ws,ye,yt,za,zm,zw"

	// Max Cantor pair: p(z, z) ~= p(25, 25) ~= 1300 bits ~= 163 bytes
	bitMap = make([]byte, 163)

	ccArr = make([]string, 247)

	ccMap    = make(map[string]struct{}, 247)
	ccNumMap = make(map[uint16]struct{}, 247)

	directArr = make([]bool, 1301, 1301)
)

func init() {
	ccs := strings.Split(countryCodes, ",")
	for _, cc := range ccs {
		idx := cantorPair(cc)
		directArr[idx] = true
		bitMap[idx/8] = setBit(bitMap[idx/8], byte(idx%8))

		ccMap[cc] = struct{}{}
		ccNumMap[idx] = struct{}{}
	}
	ccArr = ccs
}

func IsCountryCodeByDirectCantor(input string) bool {
	return directArr[cantorPair(input)]
}

func IsCountryCodeByACPM(input string) bool {
	idx := cantorPair(input)
	return hasBit(bitMap[idx/8], byte(idx%8))
}

func cantorPair(input string) uint16 {
	k1 := uint16(input[0] - 97) // Reduce unnecessary gap from ASCII [0:97]
	k2 := uint16(input[1] - 97)
	return uint16(((k1+k2)*(k1+k2+1))/2 + k2)
}

// Sets the bit at pos in the byte n.
func setBit(n byte, pos byte) byte {
	n |= (1 << pos)
	return n
}

// Checks the bit at pos in the byte n.
func hasBit(n byte, pos byte) bool {
	return (n & (1 << pos)) > 0
}

// --------------
func IsCountryCodeByArray(input string) bool {
	for _, cc := range ccArr {
		if cc == input {
			return true
		}
	}
	return false
}

// --------------
func IsCountryCodeByMapString(input string) bool {
	_, ok := ccMap[input]
	return ok
}

func IsCountryCodeByMapInt(input string) bool {
	_, ok := ccNumMap[cantorPair(input)]
	return ok
}

func detailCantor(k1, k2 int) int {
	return (k1+k2)*(k1+k2+1)/2 + k2
}

func reverseDetailCantor(z int) (x, y int) {
	w := int(math.Floor((math.Sqrt(float64(8*z+1)) - 1) / 2))
	t := (w*w + w) / 2
	y = z - t
	x = w - y
	return x, y
}
