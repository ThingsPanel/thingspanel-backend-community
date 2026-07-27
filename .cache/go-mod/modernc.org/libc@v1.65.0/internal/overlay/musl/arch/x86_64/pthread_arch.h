static inline uintptr_t __get_tp();

#ifndef __CCGO__
static inline uintptr_t __get_tp()
{
	uintptr_t tp;
	__asm__ ("mov %%fs:0,%0" : "=r" (tp) );
	return tp;
}
#endif

#define MC_PC gregs[REG_RIP]
