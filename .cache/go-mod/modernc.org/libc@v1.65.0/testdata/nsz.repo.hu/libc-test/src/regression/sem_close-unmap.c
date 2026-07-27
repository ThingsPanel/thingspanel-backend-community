// commit: f70375df85d26235a45e74559afd69be59e5ff99 2020-10-28
#define _GNU_SOURCE 1
#include <fcntl.h>
#include <stdlib.h>
#include <semaphore.h>

int main()
{
	char buf[] = "mysemXXXXXX";
	if (!mktemp(buf)) return 1;
	// open twice
	sem_t *sem = sem_open(buf, O_CREAT|O_EXCL, 0600, 0);
	sem_open(buf, 0);
	sem_unlink(buf);
	// close once
	sem_close(sem);
	// semaphore should be still mapped
	sem_post(sem);
}
