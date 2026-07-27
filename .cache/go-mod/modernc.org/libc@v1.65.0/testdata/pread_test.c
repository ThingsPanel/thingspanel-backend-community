#include <unistd.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

int main() {
    const char *filename = "test_pread_pwrite.txt";
    int fd = open(filename, O_RDWR | O_CREAT | O_TRUNC, 0644);
    if (fd == -1) {
        printf("open\n");
        return 1;
    }

    // Test data
    const char *test_data = "This is a test string.";
    size_t data_len = strlen(test_data);

    // Write initial data
    ssize_t bytes_written = write(fd, test_data, data_len);
    if (bytes_written == -1) {
        printf("write\n");
        return 1;
    }

    // Large offset test (without lseek)
    off_t large_offset = (off_t)1 << 33; // 8GB, won't fit in 32 bits
    bytes_written = pwrite(fd, "large offset", 12, large_offset);
    if (bytes_written == -1) {
        printf("pwrite (large offset)\n");
        printf("errno: %d\n", errno); 
    } else {
        printf("Data written at large offset: %lli bytes\n", bytes_written);
    }

    char buffer[13];
    ssize_t bytes_read = pread(fd, buffer, sizeof(buffer), large_offset);
    printf("bytes_read: %lli\n", bytes_read);
    if (bytes_read == -1) {
        printf("pread (large offset)\n");
        printf("errno: %d\n", errno); 
	return 1;
    } else {
        buffer[bytes_read] = '\0';
        printf("Data read from large offset: '%s'\n", buffer);
    }

    if (strcmp(buffer, "large offset")) {
        printf("pread (large offset) data mismatch (a)\n");
	return 1;
    }

    // lseek and pread/pwrite test
    if (lseek(fd, -10, SEEK_END) == -1) {
        printf("lseek\n");
        return 1;
    }

    bytes_written = pwrite(fd, "END", 3, 0); // Write "END" at the current offset
    if (bytes_written == -1) {
        printf("pwrite (after lseek)\n");
        return 1;
    }

    lseek(fd, -10, SEEK_END); // Seek back to the same position

    bytes_read = pread(fd, buffer, 3, 0);
    printf("bytes_read: %lli\n", bytes_read);
    if (bytes_read == -1) {
        printf("pread (after lseek)\n");
        return 1;
    }

    buffer[bytes_read] = '\0';
    printf("Data read after lseek: '%s'\n", buffer);

    if (strcmp(buffer, "END")) {
        printf("pread (large offset) data mismatch (b)\n");
	return 1;
    }

    close(fd);
    remove(filename);
    return 0;
}
