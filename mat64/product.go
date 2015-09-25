// Copyright ©2015 The gonum Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mat64

import "fmt"

// Product calculates the product of the given factors and places the result in
// the receiver. The order of multiplication operations is optimized to minimize
// the number of floating point operations on the basis that all matrix
// multiplications are general.
func (m *Dense) Product(factors ...Matrix) {
	// The operation order optimisation is the naive O(n^3) dynamic
	// programming approach and does not take into consideration
	// finer-grained optimisations that might be available.
	//
	// TODO(kortschak) Consider using the O(nlogn) or O(mlogn)
	// algorithms that are available. e.g.
	//
	// e.g. http://www.jofcis.com/publishedpapers/2014_10_10_4299_4306.pdf
	//
	// In the case that this is replaced, retain this code in
	// tests to compare against.

	r, c := m.Dims()
	switch len(factors) {
	case 0:
		if r != 0 || c != 0 {
			panic(ErrShape)
		}
		return
	case 1:
		m.reuseAs(factors[0].Dims())
		m.Copy(factors[0])
		return
	case 2:
		// Don't do work that we know the answer to.
		m.Mul(factors[0], factors[1])
		return
	}

	p := newMultiplier(m, factors)
	p.optimize()
	result := p.multiply()

	m.reuseAs(result.Dims())
	m.Copy(result)
	putWorkspace(result)
}

// debugProductWalk enables debugging output for Product.
const debugProductWalk = false

// multiplier performs operation order optimisation and tree traversal.
type multiplier struct {
	// factors is the ordered set of
	// factors to multiply.
	factors []Matrix
	// dims is the chain of factor
	// dimensions.
	dims []int

	// table contains the dynamic
	// programming costs and subchain
	// division indices.
	table table

	// stack holds intermediate results
	// in the product tree. onStack
	// indicates whether a matrix to
	// be multiplied is on the stack or
	// in the input factors.
	stack   []*Dense
	onStack []bool
}

func newMultiplier(m *Dense, factors []Matrix) *multiplier {
	// Check size early, but don't yet
	// allocate data for m.
	r, c := m.Dims()
	fr, fc := factors[0].Dims() // newMultiplier is only called with len(factors) > 2.
	if !m.isZero() {
		if fr != r {
			panic(ErrShape)
		}
		if _, lc := factors[len(factors)-1].Dims(); lc != c {
			panic(ErrShape)
		}
	}

	dims := make([]int, len(factors)+1)
	dims[0] = r
	dims[len(dims)-1] = c
	pc := fc
	for i, f := range factors[1:] {
		cr, cc := f.Dims()
		dims[i+1] = cr
		if pc != cr {
			panic(ErrShape)
		}
		pc = cc
	}

	return &multiplier{
		factors: factors,
		dims:    dims,
		table:   newTable(len(factors)),
		onStack: make([]bool, len(factors)),
	}
}

// optimize determines an optimal matrix multiply operation order.
func (p *multiplier) optimize() {
	if debugProductWalk {
		fmt.Printf("chain dims: %v\n", p.dims)
	}
	const maxInt = int(^uint(0) >> 1)
	for f := 1; f < len(p.factors); f++ {
		for i := 0; i < len(p.factors)-f; i++ {
			j := i + f
			p.table.set(i, j, entry{cost: maxInt})
			for k := i; k < j; k++ {
				cost := p.table.at(i, k).cost + p.table.at(k+1, j).cost + p.dims[i]*p.dims[k+1]*p.dims[j+1]
				if cost < p.table.at(i, j).cost {
					p.table.set(i, j, entry{cost: cost, k: k})
				}
			}
		}
	}
}

// multiply walks the optimal operation tree found by optimize,
// leaving the final result in the stack. It returns the
// product, which may be copied but should be returned to
// the workspace pool.
func (p *multiplier) multiply() *Dense {
	p.multiplySubchain(0, len(p.factors)-1)
	if debugProductWalk {
		r, c := p.stack[0].Dims()
		fmt.Printf("\tpop result (%d×%d) cost=%d\n", r, c, p.table.at(0, len(p.factors)-1).cost)
	}
	return p.stack[0]
}

func (p *multiplier) multiplySubchain(i, j int) {
	if i == j {
		return
	}

	p.multiplySubchain(i, p.table.at(i, j).k)
	p.multiplySubchain(p.table.at(i, j).k+1, j)

	b := p.factor(j)
	a := p.factor(i)
	ar, ac := a.Dims()
	br, bc := b.Dims()
	if ac != br {
		// Panic with a string since this
		// is not a user-facing panic.
		panic(ErrShape.string)
	}

	if debugProductWalk {
		ar, ac := a.Dims()
		br, bc := b.Dims()
		fmt.Printf("\tpush f[%d] (%d×%d)%s * f[%d] (%d×%d)%s\n",
			i, ar, ac, result(p.onStack[i]), j, br, bc, result(p.onStack[j]))
	}

	r := getWorkspace(ar, bc, false)
	r.Mul(a, b)
	if p.onStack[i] {
		putWorkspace(a.(*Dense))
	}
	if p.onStack[j] {
		putWorkspace(b.(*Dense))
	}
	p.push(r, i, j)
}

func (p *multiplier) push(m *Dense, i, j int) {
	p.onStack[i] = true
	p.onStack[j] = true
	p.stack = append(p.stack, m)
}

func (p *multiplier) factor(i int) Matrix {
	if !p.onStack[i] {
		return p.factors[i]
	}
	var m *Dense
	m, p.stack = p.stack[len(p.stack)-1], p.stack[:len(p.stack)-1]
	return m
}

type entry struct {
	k    int // is the chain subdivision index.
	cost int // cost is the cost of the operation.
}

// table is a row major n×n dynamic programming table.
type table struct {
	n       int
	entries []entry
}

func newTable(n int) table {
	return table{n: n, entries: make([]entry, n*n)}
}

func (t table) at(i, j int) entry     { return t.entries[i*t.n+j] }
func (t table) set(i, j int, e entry) { t.entries[i*t.n+j] = e }

type result bool

func (r result) String() string {
	if r {
		return " (popped result)"
	}
	return ""
}
