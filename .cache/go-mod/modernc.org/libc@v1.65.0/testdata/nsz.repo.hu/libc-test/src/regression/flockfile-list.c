// commit: 3e936ce81bbbcc968f576aedbd5203621839f152 2014-09-19
// flockfile linked list handling was broken
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "test.h"

#define t_fatal(...) (t_error(__VA_ARGS__), _Exit(t_status))
#define length(a) (sizeof(a)/sizeof*(a))

// interpose malloc functions
// freed memory is not reused, it is checked for clobber.

static unsigned char buf[1<<20];
static size_t pos;
static struct {
	size_t pos;
	size_t n;
	int freed;
} alloc[100];
static int idx;

void *malloc(size_t n)
{
	if (n == 0) n++;
	if (n > sizeof buf - pos)
		t_fatal("test buffer is small, pos: %zu, need: %zu\n", pos, n);
	if (idx >= length(alloc))
		t_fatal("test buffer is small, idx: %d\n", idx);
	void *p = buf + pos;
	alloc[idx].pos = pos;
	alloc[idx].n = n;
	pos += n;
	idx++;
	return p;
}

void *calloc(size_t n, size_t m)
{
	return memset(malloc(n*m), 0, n*m);
}

void *aligned_alloc(size_t a, size_t n)
{
	t_fatal("aligned_alloc is unsupported\n");
}

static int findidx(void *p)
{
	size_t pos = (unsigned char *)p - buf;
	for (int i=0; i<idx; i++)
		if (alloc[i].pos == pos)
			return i;
	t_fatal("%p is not an allocated pointer\n", p);
	return -1;
}

void *realloc(void *p, size_t n)
{
	void *q = malloc(n);
	size_t m = alloc[findidx(p)].n;
	memcpy(q, p, m < n ? m : n);
	free(p);
	return q;
}

void free(void *p)
{
	if (p == 0) return;
	int i = findidx(p);
	memset(p, 42, alloc[i].n);
	alloc[i].freed = 1;
}

static void checkfreed(void)
{
	for (int i=0; i<idx; i++)
		if (alloc[i].freed)
			for (size_t j=0; j<alloc[i].n; j++)
				if (buf[alloc[i].pos + j] != 42) {
					t_error("freed allocation %d (pos: %zu, len: %zu) is clobbered\n", i, alloc[i].pos, alloc[i].n);
					break;
				}
}

int main()
{
	FILE *f = tmpfile();
	FILE *g = tmpfile();
	flockfile(g);
	flockfile(f);
	funlockfile(g);
	fclose(g);
	/* may corrupt memory */
	funlockfile(f);
	checkfreed();
	return t_status;
}
