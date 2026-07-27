// Code generated for windows/386 by 'ccgo -o ../vfs_windows_386.go --goos=windows --goarch=386 --cpp=/usr/bin/i686-w64-mingw32-cpp vfs.c -I /home/jnml/src/modernc.org/libsqlite3/sqlite-amalgamation-3460000 -lsqlite3 --package-name vfs --prefix-external=X -DSQLITE_OS_WIN -hide=vfsFullPathname -hide=vfsOpen -hide=vfsRead -hide=vfsAccess -hide=vfsFileSize -hide=vfsClose -ignore-link-errors --prefix-field '

//go:build windows && 386

package vfs

import (
	"reflect"
	"time"
	"unsafe"

	"modernc.org/libc"
	"modernc.org/libc/sys/types"
	libsqlite3 "modernc.org/sqlite/lib"
)

var (
	_ reflect.Type
	_ unsafe.Pointer
)

const BIG_ENDIAN = 4321
const BYTE_ORDER = "LITTLE_ENDIAN"
const CLK_TCK = "CLOCKS_PER_SEC"
const CLOCKS_PER_SEC = 1000
const CLOCK_MONOTONIC = 1
const CLOCK_PROCESS_CPUTIME_ID = 2
const CLOCK_REALTIME = 0
const CLOCK_REALTIME_COARSE = 4
const CLOCK_THREAD_CPUTIME_ID = 3
const E2BIG = 7
const EACCES = 13
const EADDRINUSE = 100
const EADDRNOTAVAIL = 101
const EAFNOSUPPORT = 102
const EAGAIN = 11
const EALREADY = 103
const EBADF = 9
const EBADMSG = 104
const EBUSY = 16
const ECANCELED = 105
const ECHILD = 10
const ECONNABORTED = 106
const ECONNREFUSED = 107
const ECONNRESET = 108
const EDEADLK = 36
const EDEADLOCK = "EDEADLK"
const EDESTADDRREQ = 109
const EDOM = 33
const EEXIST = 17
const EFAULT = 14
const EFBIG = 27
const EHOSTUNREACH = 110
const EIDRM = 111
const EILSEQ = 42
const EINPROGRESS = 112
const EINTR = 4
const EINVAL = 22
const EIO = 5
const EISCONN = 113
const EISDIR = 21
const ELOOP = 114
const EMFILE = 24
const EMLINK = 31
const EMSGSIZE = 115
const ENAMETOOLONG = 38
const ENETDOWN = 116
const ENETRESET = 117
const ENETUNREACH = 118
const ENFILE = 23
const ENOBUFS = 119
const ENODATA = 120
const ENODEV = 19
const ENOENT = 2
const ENOEXEC = 8
const ENOFILE = "ENOENT"
const ENOLCK = 39
const ENOLINK = 121
const ENOMEM = 12
const ENOMSG = 122
const ENOPROTOOPT = 123
const ENOSPC = 28
const ENOSR = 124
const ENOSTR = 125
const ENOSYS = 40
const ENOTCONN = 126
const ENOTDIR = 20
const ENOTEMPTY = 41
const ENOTRECOVERABLE = 127
const ENOTSOCK = 128
const ENOTSUP = 129
const ENOTTY = 25
const ENXIO = 6
const EOPNOTSUPP = 130
const EOVERFLOW = 132
const EOWNERDEAD = 133
const EPERM = 1
const EPIPE = 32
const EPROTO = 134
const EPROTONOSUPPORT = 135
const EPROTOTYPE = 136
const ERANGE = 34
const EROFS = 30
const ESPIPE = 29
const ESRCH = 3
const ETIME = 137
const ETIMEDOUT = 138
const ETXTBSY = 139
const EWOULDBLOCK = 140
const EXDEV = 18
const FTS5_TOKENIZE_AUX = 0x0008
const FTS5_TOKENIZE_DOCUMENT = 0x0004
const FTS5_TOKENIZE_PREFIX = 0x0002
const FTS5_TOKENIZE_QUERY = 0x0001
const FTS5_TOKEN_COLOCATED = 0x0001
const FULLY_WITHIN = 2
const F_OK = 0
const LITTLE_ENDIAN = 1234
const MAXPATHLEN = "PATH_MAX"
const MAXPATHNAME = 4096
const MB_LEN_MAX = 5
const MINGW_HAS_DDK_H = 1
const MINGW_HAS_SECURE_API = 1
const NOT_WITHIN = 0
const OLD_P_OVERLAY = "_OLD_P_OVERLAY"
const O_ACCMODE = "_O_ACCMODE"
const O_APPEND = "_O_APPEND"
const O_BINARY = "_O_BINARY"
const O_CREAT = "_O_CREAT"
const O_EXCL = "_O_EXCL"
const O_NOINHERIT = "_O_NOINHERIT"
const O_RANDOM = "_O_RANDOM"
const O_RAW = "_O_BINARY"
const O_RDONLY = "_O_RDONLY"
const O_RDWR = "_O_RDWR"
const O_SEQUENTIAL = "_O_SEQUENTIAL"
const O_TEMPORARY = "_O_TEMPORARY"
const O_TEXT = "_O_TEXT"
const O_TRUNC = "_O_TRUNC"
const O_WRONLY = "_O_WRONLY"
const PARTLY_WITHIN = 1
const PATH_MAX = 260
const P_DETACH = "_P_DETACH"
const P_NOWAIT = "_P_NOWAIT"
const P_NOWAITO = "_P_NOWAITO"
const P_OVERLAY = "_P_OVERLAY"
const P_WAIT = "_P_WAIT"
const R_OK = 4
const SEEK_CUR = 1
const SEEK_END = 2
const SEEK_SET = 0
const SIZE_MAX = "UINT_MAX"
const SQLITE3_TEXT = 3
const SQLITE_ABORT = 4
const SQLITE_ACCESS_EXISTS = 0
const SQLITE_ACCESS_READ = 2
const SQLITE_ACCESS_READWRITE = 1
const SQLITE_ALTER_TABLE = 26
const SQLITE_ANALYZE = 28
const SQLITE_ANY = 5
const SQLITE_ATTACH = 24
const SQLITE_AUTH = 23
const SQLITE_BLOB = 4
const SQLITE_BUSY = 5
const SQLITE_CANTOPEN = 14
const SQLITE_CHECKPOINT_FULL = 1
const SQLITE_CHECKPOINT_PASSIVE = 0
const SQLITE_CHECKPOINT_RESTART = 2
const SQLITE_CHECKPOINT_TRUNCATE = 3
const SQLITE_CONFIG_COVERING_INDEX_SCAN = 20
const SQLITE_CONFIG_GETMALLOC = 5
const SQLITE_CONFIG_GETMUTEX = 11
const SQLITE_CONFIG_GETPCACHE = 15
const SQLITE_CONFIG_GETPCACHE2 = 19
const SQLITE_CONFIG_HEAP = 8
const SQLITE_CONFIG_LOG = 16
const SQLITE_CONFIG_LOOKASIDE = 13
const SQLITE_CONFIG_MALLOC = 4
const SQLITE_CONFIG_MEMDB_MAXSIZE = 29
const SQLITE_CONFIG_MEMSTATUS = 9
const SQLITE_CONFIG_MMAP_SIZE = 22
const SQLITE_CONFIG_MULTITHREAD = 2
const SQLITE_CONFIG_MUTEX = 10
const SQLITE_CONFIG_PAGECACHE = 7
const SQLITE_CONFIG_PCACHE = 14
const SQLITE_CONFIG_PCACHE2 = 18
const SQLITE_CONFIG_PCACHE_HDRSZ = 24
const SQLITE_CONFIG_PMASZ = 25
const SQLITE_CONFIG_ROWID_IN_VIEW = 30
const SQLITE_CONFIG_SCRATCH = 6
const SQLITE_CONFIG_SERIALIZED = 3
const SQLITE_CONFIG_SINGLETHREAD = 1
const SQLITE_CONFIG_SMALL_MALLOC = 27
const SQLITE_CONFIG_SORTERREF_SIZE = 28
const SQLITE_CONFIG_SQLLOG = 21
const SQLITE_CONFIG_STMTJRNL_SPILL = 26
const SQLITE_CONFIG_URI = 17
const SQLITE_CONFIG_WIN32_HEAPSIZE = 23
const SQLITE_CONSTRAINT = 19
const SQLITE_COPY = 0
const SQLITE_CORRUPT = 11
const SQLITE_CREATE_INDEX = 1
const SQLITE_CREATE_TABLE = 2
const SQLITE_CREATE_TEMP_INDEX = 3
const SQLITE_CREATE_TEMP_TABLE = 4
const SQLITE_CREATE_TEMP_TRIGGER = 5
const SQLITE_CREATE_TEMP_VIEW = 6
const SQLITE_CREATE_TRIGGER = 7
const SQLITE_CREATE_VIEW = 8
const SQLITE_CREATE_VTABLE = 29
const SQLITE_DBCONFIG_DEFENSIVE = 1010
const SQLITE_DBCONFIG_DQS_DDL = 1014
const SQLITE_DBCONFIG_DQS_DML = 1013
const SQLITE_DBCONFIG_ENABLE_FKEY = 1002
const SQLITE_DBCONFIG_ENABLE_FTS3_TOKENIZER = 1004
const SQLITE_DBCONFIG_ENABLE_LOAD_EXTENSION = 1005
const SQLITE_DBCONFIG_ENABLE_QPSG = 1007
const SQLITE_DBCONFIG_ENABLE_TRIGGER = 1003
const SQLITE_DBCONFIG_ENABLE_VIEW = 1015
const SQLITE_DBCONFIG_LEGACY_ALTER_TABLE = 1012
const SQLITE_DBCONFIG_LEGACY_FILE_FORMAT = 1016
const SQLITE_DBCONFIG_LOOKASIDE = 1001
const SQLITE_DBCONFIG_MAINDBNAME = 1000
const SQLITE_DBCONFIG_MAX = 1019
const SQLITE_DBCONFIG_NO_CKPT_ON_CLOSE = 1006
const SQLITE_DBCONFIG_RESET_DATABASE = 1009
const SQLITE_DBCONFIG_REVERSE_SCANORDER = 1019
const SQLITE_DBCONFIG_STMT_SCANSTATUS = 1018
const SQLITE_DBCONFIG_TRIGGER_EQP = 1008
const SQLITE_DBCONFIG_TRUSTED_SCHEMA = 1017
const SQLITE_DBCONFIG_WRITABLE_SCHEMA = 1011
const SQLITE_DBSTATUS_CACHE_HIT = 7
const SQLITE_DBSTATUS_CACHE_MISS = 8
const SQLITE_DBSTATUS_CACHE_SPILL = 12
const SQLITE_DBSTATUS_CACHE_USED = 1
const SQLITE_DBSTATUS_CACHE_USED_SHARED = 11
const SQLITE_DBSTATUS_CACHE_WRITE = 9
const SQLITE_DBSTATUS_DEFERRED_FKS = 10
const SQLITE_DBSTATUS_LOOKASIDE_HIT = 4
const SQLITE_DBSTATUS_LOOKASIDE_MISS_FULL = 6
const SQLITE_DBSTATUS_LOOKASIDE_MISS_SIZE = 5
const SQLITE_DBSTATUS_LOOKASIDE_USED = 0
const SQLITE_DBSTATUS_MAX = 12
const SQLITE_DBSTATUS_SCHEMA_USED = 2
const SQLITE_DBSTATUS_STMT_USED = 3
const SQLITE_DELETE = 9
const SQLITE_DENY = 1
const SQLITE_DESERIALIZE_FREEONCLOSE = 1
const SQLITE_DESERIALIZE_READONLY = 4
const SQLITE_DESERIALIZE_RESIZEABLE = 2
const SQLITE_DETACH = 25
const SQLITE_DETERMINISTIC = 0x000000800
const SQLITE_DIRECTONLY = 0x000080000
const SQLITE_DONE = 101
const SQLITE_DROP_INDEX = 10
const SQLITE_DROP_TABLE = 11
const SQLITE_DROP_TEMP_INDEX = 12
const SQLITE_DROP_TEMP_TABLE = 13
const SQLITE_DROP_TEMP_TRIGGER = 14
const SQLITE_DROP_TEMP_VIEW = 15
const SQLITE_DROP_TRIGGER = 16
const SQLITE_DROP_VIEW = 17
const SQLITE_DROP_VTABLE = 30
const SQLITE_EMPTY = 16
const SQLITE_ERROR = 1
const SQLITE_EXTERN = "extern"
const SQLITE_FAIL = 3
const SQLITE_FCNTL_BEGIN_ATOMIC_WRITE = 31
const SQLITE_FCNTL_BUSYHANDLER = 15
const SQLITE_FCNTL_CHUNK_SIZE = 6
const SQLITE_FCNTL_CKPT_DONE = 37
const SQLITE_FCNTL_CKPT_START = 39
const SQLITE_FCNTL_CKSM_FILE = 41
const SQLITE_FCNTL_COMMIT_ATOMIC_WRITE = 32
const SQLITE_FCNTL_COMMIT_PHASETWO = 22
const SQLITE_FCNTL_DATA_VERSION = 35
const SQLITE_FCNTL_EXTERNAL_READER = 40
const SQLITE_FCNTL_FILE_POINTER = 7
const SQLITE_FCNTL_GET_LOCKPROXYFILE = 2
const SQLITE_FCNTL_HAS_MOVED = 20
const SQLITE_FCNTL_JOURNAL_POINTER = 28
const SQLITE_FCNTL_LAST_ERRNO = 4
const SQLITE_FCNTL_LOCKSTATE = 1
const SQLITE_FCNTL_LOCK_TIMEOUT = 34
const SQLITE_FCNTL_MMAP_SIZE = 18
const SQLITE_FCNTL_OVERWRITE = 11
const SQLITE_FCNTL_PDB = 30
const SQLITE_FCNTL_PERSIST_WAL = 10
const SQLITE_FCNTL_POWERSAFE_OVERWRITE = 13
const SQLITE_FCNTL_PRAGMA = 14
const SQLITE_FCNTL_RBU = 26
const SQLITE_FCNTL_RESERVE_BYTES = 38
const SQLITE_FCNTL_RESET_CACHE = 42
const SQLITE_FCNTL_ROLLBACK_ATOMIC_WRITE = 33
const SQLITE_FCNTL_SET_LOCKPROXYFILE = 3
const SQLITE_FCNTL_SIZE_HINT = 5
const SQLITE_FCNTL_SIZE_LIMIT = 36
const SQLITE_FCNTL_SYNC = 21
const SQLITE_FCNTL_SYNC_OMITTED = 8
const SQLITE_FCNTL_TEMPFILENAME = 16
const SQLITE_FCNTL_TRACE = 19
const SQLITE_FCNTL_VFSNAME = 12
const SQLITE_FCNTL_VFS_POINTER = 27
const SQLITE_FCNTL_WAL_BLOCK = 24
const SQLITE_FCNTL_WIN32_AV_RETRY = 9
const SQLITE_FCNTL_WIN32_GET_HANDLE = 29
const SQLITE_FCNTL_WIN32_SET_HANDLE = 23
const SQLITE_FCNTL_ZIPVFS = 25
const SQLITE_FLOAT = 2
const SQLITE_FORMAT = 24
const SQLITE_FULL = 13
const SQLITE_FUNCTION = 31
const SQLITE_GET_LOCKPROXYFILE = "SQLITE_FCNTL_GET_LOCKPROXYFILE"
const SQLITE_IGNORE = 2
const SQLITE_INDEX_CONSTRAINT_EQ = 2
const SQLITE_INDEX_CONSTRAINT_FUNCTION = 150
const SQLITE_INDEX_CONSTRAINT_GE = 32
const SQLITE_INDEX_CONSTRAINT_GLOB = 66
const SQLITE_INDEX_CONSTRAINT_GT = 4
const SQLITE_INDEX_CONSTRAINT_IS = 72
const SQLITE_INDEX_CONSTRAINT_ISNOT = 69
const SQLITE_INDEX_CONSTRAINT_ISNOTNULL = 70
const SQLITE_INDEX_CONSTRAINT_ISNULL = 71
const SQLITE_INDEX_CONSTRAINT_LE = 8
const SQLITE_INDEX_CONSTRAINT_LIKE = 65
const SQLITE_INDEX_CONSTRAINT_LIMIT = 73
const SQLITE_INDEX_CONSTRAINT_LT = 16
const SQLITE_INDEX_CONSTRAINT_MATCH = 64
const SQLITE_INDEX_CONSTRAINT_NE = 68
const SQLITE_INDEX_CONSTRAINT_OFFSET = 74
const SQLITE_INDEX_CONSTRAINT_REGEXP = 67
const SQLITE_INDEX_SCAN_UNIQUE = 1
const SQLITE_INNOCUOUS = 0x000200000
const SQLITE_INSERT = 18
const SQLITE_INTEGER = 1
const SQLITE_INTERNAL = 2
const SQLITE_INTERRUPT = 9
const SQLITE_IOCAP_ATOMIC = 0x00000001
const SQLITE_IOCAP_ATOMIC16K = 0x00000040
const SQLITE_IOCAP_ATOMIC1K = 0x00000004
const SQLITE_IOCAP_ATOMIC2K = 0x00000008
const SQLITE_IOCAP_ATOMIC32K = 0x00000080
const SQLITE_IOCAP_ATOMIC4K = 0x00000010
const SQLITE_IOCAP_ATOMIC512 = 0x00000002
const SQLITE_IOCAP_ATOMIC64K = 0x00000100
const SQLITE_IOCAP_ATOMIC8K = 0x00000020
const SQLITE_IOCAP_BATCH_ATOMIC = 0x00004000
const SQLITE_IOCAP_IMMUTABLE = 0x00002000
const SQLITE_IOCAP_POWERSAFE_OVERWRITE = 0x00001000
const SQLITE_IOCAP_SAFE_APPEND = 0x00000200
const SQLITE_IOCAP_SEQUENTIAL = 0x00000400
const SQLITE_IOCAP_UNDELETABLE_WHEN_OPEN = 0x00000800
const SQLITE_IOERR = 10
const SQLITE_LAST_ERRNO = "SQLITE_FCNTL_LAST_ERRNO"
const SQLITE_LIMIT_ATTACHED = 7
const SQLITE_LIMIT_COLUMN = 2
const SQLITE_LIMIT_COMPOUND_SELECT = 4
const SQLITE_LIMIT_EXPR_DEPTH = 3
const SQLITE_LIMIT_FUNCTION_ARG = 6
const SQLITE_LIMIT_LENGTH = 0
const SQLITE_LIMIT_LIKE_PATTERN_LENGTH = 8
const SQLITE_LIMIT_SQL_LENGTH = 1
const SQLITE_LIMIT_TRIGGER_DEPTH = 10
const SQLITE_LIMIT_VARIABLE_NUMBER = 9
const SQLITE_LIMIT_VDBE_OP = 5
const SQLITE_LIMIT_WORKER_THREADS = 11
const SQLITE_LOCKED = 6
const SQLITE_LOCK_EXCLUSIVE = 4
const SQLITE_LOCK_NONE = 0
const SQLITE_LOCK_PENDING = 3
const SQLITE_LOCK_RESERVED = 2
const SQLITE_LOCK_SHARED = 1
const SQLITE_MISMATCH = 20
const SQLITE_MISUSE = 21
const SQLITE_MUTEX_FAST = 0
const SQLITE_MUTEX_RECURSIVE = 1
const SQLITE_MUTEX_STATIC_APP1 = 8
const SQLITE_MUTEX_STATIC_APP2 = 9
const SQLITE_MUTEX_STATIC_APP3 = 10
const SQLITE_MUTEX_STATIC_LRU = 6
const SQLITE_MUTEX_STATIC_LRU2 = 7
const SQLITE_MUTEX_STATIC_MAIN = 2
const SQLITE_MUTEX_STATIC_MASTER = 2
const SQLITE_MUTEX_STATIC_MEM = 3
const SQLITE_MUTEX_STATIC_MEM2 = 4
const SQLITE_MUTEX_STATIC_OPEN = 4
const SQLITE_MUTEX_STATIC_PMEM = 7
const SQLITE_MUTEX_STATIC_PRNG = 5
const SQLITE_MUTEX_STATIC_VFS1 = 11
const SQLITE_MUTEX_STATIC_VFS2 = 12
const SQLITE_MUTEX_STATIC_VFS3 = 13
const SQLITE_NOLFS = 22
const SQLITE_NOMEM = 7
const SQLITE_NOTADB = 26
const SQLITE_NOTFOUND = 12
const SQLITE_NOTICE = 27
const SQLITE_NULL = 5
const SQLITE_OK = 0
const SQLITE_OPEN_AUTOPROXY = 0x00000020
const SQLITE_OPEN_CREATE = 4
const SQLITE_OPEN_DELETEONCLOSE = 0x00000008
const SQLITE_OPEN_EXCLUSIVE = 16
const SQLITE_OPEN_EXRESCODE = 0x02000000
const SQLITE_OPEN_FULLMUTEX = 0x00010000
const SQLITE_OPEN_MAIN_DB = 0x00000100
const SQLITE_OPEN_MAIN_JOURNAL = 2048
const SQLITE_OPEN_MASTER_JOURNAL = 0x00004000
const SQLITE_OPEN_MEMORY = 0x00000080
const SQLITE_OPEN_NOFOLLOW = 0x01000000
const SQLITE_OPEN_NOMUTEX = 0x00008000
const SQLITE_OPEN_PRIVATECACHE = 0x00040000
const SQLITE_OPEN_READONLY = 1
const SQLITE_OPEN_READWRITE = 2
const SQLITE_OPEN_SHAREDCACHE = 0x00020000
const SQLITE_OPEN_SUBJOURNAL = 0x00002000
const SQLITE_OPEN_SUPER_JOURNAL = 0x00004000
const SQLITE_OPEN_TEMP_DB = 0x00000200
const SQLITE_OPEN_TEMP_JOURNAL = 0x00001000
const SQLITE_OPEN_TRANSIENT_DB = 0x00000400
const SQLITE_OPEN_URI = 0x00000040
const SQLITE_OPEN_WAL = 0x00080000
const SQLITE_OS_WIN = 1
const SQLITE_PERM = 3
const SQLITE_PRAGMA = 19
const SQLITE_PREPARE_NORMALIZE = 0x02
const SQLITE_PREPARE_NO_VTAB = 0x04
const SQLITE_PREPARE_PERSISTENT = 0x01
const SQLITE_PROTOCOL = 15
const SQLITE_RANGE = 25
const SQLITE_READ = 20
const SQLITE_READONLY = 8
const SQLITE_RECURSIVE = 33
const SQLITE_REINDEX = 27
const SQLITE_REPLACE = 5
const SQLITE_RESULT_SUBTYPE = 0x001000000
const SQLITE_ROLLBACK = 1
const SQLITE_ROW = 100
const SQLITE_SAVEPOINT = 32
const SQLITE_SCANSTAT_COMPLEX = 0x0001
const SQLITE_SCANSTAT_EST = 2
const SQLITE_SCANSTAT_EXPLAIN = 4
const SQLITE_SCANSTAT_NAME = 3
const SQLITE_SCANSTAT_NCYCLE = 7
const SQLITE_SCANSTAT_NLOOP = 0
const SQLITE_SCANSTAT_NVISIT = 1
const SQLITE_SCANSTAT_PARENTID = 6
const SQLITE_SCANSTAT_SELECTID = 5
const SQLITE_SCHEMA = 17
const SQLITE_SELECT = 21
const SQLITE_SERIALIZE_NOCOPY = 0x001
const SQLITE_SET_LOCKPROXYFILE = "SQLITE_FCNTL_SET_LOCKPROXYFILE"
const SQLITE_SHM_EXCLUSIVE = 8
const SQLITE_SHM_LOCK = 2
const SQLITE_SHM_NLOCK = 8
const SQLITE_SHM_SHARED = 4
const SQLITE_SHM_UNLOCK = 1
const SQLITE_SOURCE_ID = "2024-05-23 13:25:27 96c92aba00c8375bc32fafcdf12429c58bd8aabfcadab6683e35bbb9cdebf19e"
const SQLITE_STATUS_MALLOC_COUNT = 9
const SQLITE_STATUS_MALLOC_SIZE = 5
const SQLITE_STATUS_MEMORY_USED = 0
const SQLITE_STATUS_PAGECACHE_OVERFLOW = 2
const SQLITE_STATUS_PAGECACHE_SIZE = 7
const SQLITE_STATUS_PAGECACHE_USED = 1
const SQLITE_STATUS_PARSER_STACK = 6
const SQLITE_STATUS_SCRATCH_OVERFLOW = 4
const SQLITE_STATUS_SCRATCH_SIZE = 8
const SQLITE_STATUS_SCRATCH_USED = 3
const SQLITE_STDCALL = "SQLITE_APICALL"
const SQLITE_STMTSTATUS_AUTOINDEX = 3
const SQLITE_STMTSTATUS_FILTER_HIT = 8
const SQLITE_STMTSTATUS_FILTER_MISS = 7
const SQLITE_STMTSTATUS_FULLSCAN_STEP = 1
const SQLITE_STMTSTATUS_MEMUSED = 99
const SQLITE_STMTSTATUS_REPREPARE = 5
const SQLITE_STMTSTATUS_RUN = 6
const SQLITE_STMTSTATUS_SORT = 2
const SQLITE_STMTSTATUS_VM_STEP = 4
const SQLITE_SUBTYPE = 0x000100000
const SQLITE_SYNC_DATAONLY = 0x00010
const SQLITE_SYNC_FULL = 0x00003
const SQLITE_SYNC_NORMAL = 0x00002
const SQLITE_TESTCTRL_ALWAYS = 13
const SQLITE_TESTCTRL_ASSERT = 12
const SQLITE_TESTCTRL_BENIGN_MALLOC_HOOKS = 10
const SQLITE_TESTCTRL_BITVEC_TEST = 8
const SQLITE_TESTCTRL_BYTEORDER = 22
const SQLITE_TESTCTRL_EXPLAIN_STMT = 19
const SQLITE_TESTCTRL_EXTRA_SCHEMA_CHECKS = 29
const SQLITE_TESTCTRL_FAULT_INSTALL = 9
const SQLITE_TESTCTRL_FIRST = 5
const SQLITE_TESTCTRL_FK_NO_ACTION = 7
const SQLITE_TESTCTRL_IMPOSTER = 25
const SQLITE_TESTCTRL_INTERNAL_FUNCTIONS = 17
const SQLITE_TESTCTRL_ISINIT = 23
const SQLITE_TESTCTRL_ISKEYWORD = 16
const SQLITE_TESTCTRL_JSON_SELFCHECK = 14
const SQLITE_TESTCTRL_LAST = 34
const SQLITE_TESTCTRL_LOCALTIME_FAULT = 18
const SQLITE_TESTCTRL_LOGEST = 33
const SQLITE_TESTCTRL_NEVER_CORRUPT = 20
const SQLITE_TESTCTRL_ONCE_RESET_THRESHOLD = 19
const SQLITE_TESTCTRL_OPTIMIZATIONS = 15
const SQLITE_TESTCTRL_PARSER_COVERAGE = 26
const SQLITE_TESTCTRL_PENDING_BYTE = 11
const SQLITE_TESTCTRL_PRNG_RESET = 7
const SQLITE_TESTCTRL_PRNG_RESTORE = 6
const SQLITE_TESTCTRL_PRNG_SAVE = 5
const SQLITE_TESTCTRL_PRNG_SEED = 28
const SQLITE_TESTCTRL_RESERVE = 14
const SQLITE_TESTCTRL_RESULT_INTREAL = 27
const SQLITE_TESTCTRL_SCRATCHMALLOC = 17
const SQLITE_TESTCTRL_SEEK_COUNT = 30
const SQLITE_TESTCTRL_SORTER_MMAP = 24
const SQLITE_TESTCTRL_TRACEFLAGS = 31
const SQLITE_TESTCTRL_TUNE = 32
const SQLITE_TESTCTRL_USELONGDOUBLE = 34
const SQLITE_TESTCTRL_VDBE_COVERAGE = 21
const SQLITE_TEXT = 3
const SQLITE_TOOBIG = 18
const SQLITE_TRACE_CLOSE = 0x08
const SQLITE_TRACE_PROFILE = 0x02
const SQLITE_TRACE_ROW = 0x04
const SQLITE_TRACE_STMT = 0x01
const SQLITE_TRANSACTION = 22
const SQLITE_TXN_NONE = 0
const SQLITE_TXN_READ = 1
const SQLITE_TXN_WRITE = 2
const SQLITE_UPDATE = 23
const SQLITE_UTF16 = 4
const SQLITE_UTF16BE = 3
const SQLITE_UTF16LE = 2
const SQLITE_UTF16_ALIGNED = 8
const SQLITE_UTF8 = 1
const SQLITE_VERSION = "3.46.0"
const SQLITE_VERSION_NUMBER = 3046000
const SQLITE_VFS_BUFFERSZ = 8192
const SQLITE_VTAB_CONSTRAINT_SUPPORT = 1
const SQLITE_VTAB_DIRECTONLY = 3
const SQLITE_VTAB_INNOCUOUS = 2
const SQLITE_VTAB_USES_ALL_SCHEMAS = 4
const SQLITE_WARNING = 28
const SQLITE_WIN32_DATA_DIRECTORY_TYPE = 1
const SQLITE_WIN32_TEMP_DIRECTORY_TYPE = 2
const SSIZE_MAX = "INT_MAX"
const STDERR_FILENO = 2
const STDIN_FILENO = 0
const STDOUT_FILENO = 1
const STRUNCATE = 80
const S_IEXEC = "_S_IEXEC"
const S_IFBLK = "_S_IFBLK"
const S_IFCHR = "_S_IFCHR"
const S_IFDIR = "_S_IFDIR"
const S_IFIFO = "_S_IFIFO"
const S_IFMT = "_S_IFMT"
const S_IFREG = "_S_IFREG"
const S_IREAD = "_S_IREAD"
const S_IRUSR = "_S_IRUSR"
const S_IRWXU = "_S_IRWXU"
const S_IWRITE = "_S_IWRITE"
const S_IWUSR = "_S_IWUSR"
const S_IXUSR = "_S_IXUSR"
const TIMER_ABSTIME = 1
const USE___UUIDOF = 0
const WAIT_CHILD = "_WAIT_CHILD"
const WAIT_GRANDCHILD = "_WAIT_GRANDCHILD"
const WIN32 = 1
const WINNT = 1
const W_OK = 2
const X_OK = 1
const _ANONYMOUS_STRUCT = "__MINGW_EXTENSION"
const _ANONYMOUS_UNION = "__MINGW_EXTENSION"
const _ARGMAX = 100
const _A_ARCH = 0x20
const _A_HIDDEN = 0x02
const _A_NORMAL = 0x00
const _A_RDONLY = 0x01
const _A_SUBDIR = 0x10
const _A_SYSTEM = 0x04
const _CRTIMP2 = "_CRTIMP"
const _CRTIMP_ALTERNATIVE = "_CRTIMP"
const _CRTIMP_NOIA64 = "_CRTIMP"
const _CRTIMP_PURE = "_CRTIMP"
const _FILE_OFFSET_BITS = 64
const _I16_MAX = 32767
const _I32_MAX = 2147483647
const _I64_MAX = "9223372036854775807ll"
const _I8_MAX = 127
const _ILP32 = 1
const _INTEGRAL_MAX_BITS = 64
const _MCRTIMP = "_CRTIMP"
const _MRTIMP2 = "_CRTIMP"
const _M_IX86 = 600
const _NLSCMPERROR = 2147483647
const _OLD_P_OVERLAY = 2
const _O_APPEND = 0x0008
const _O_BINARY = 0x8000
const _O_CREAT = 256
const _O_EXCL = 1024
const _O_NOINHERIT = 0x0080
const _O_RANDOM = 0x0010
const _O_RAW = "_O_BINARY"
const _O_RDONLY = 0
const _O_RDWR = 2
const _O_SEQUENTIAL = 0x0020
const _O_SHORT_LIVED = 0x1000
const _O_TEMPORARY = 0x0040
const _O_TEXT = 0x4000
const _O_TRUNC = 0x0200
const _O_U16TEXT = 0x20000
const _O_U8TEXT = 0x40000
const _O_WRONLY = 0x0001
const _O_WTEXT = 0x10000
const _POSIX_CPUTIME = 200809
const _POSIX_MONOTONIC_CLOCK = 200809
const _POSIX_THREAD_CPUTIME = 200809
const _POSIX_TIMERS = 200809
const _P_DETACH = 4
const _P_NOWAIT = 1
const _P_NOWAITO = 3
const _P_OVERLAY = 2
const _P_WAIT = 0
const _SECURECRT_FILL_BUFFER_PATTERN = 0xFD
const _S_IEXEC = 0x0040
const _S_IFBLK = 0x3000
const _S_IFCHR = 0x2000
const _S_IFDIR = 0x4000
const _S_IFIFO = 0x1000
const _S_IFMT = 0xF000
const _S_IFREG = 0x8000
const _S_IREAD = 0x0100
const _S_IRUSR = "_S_IREAD"
const _S_IWRITE = 0x0080
const _S_IWUSR = "_S_IWRITE"
const _S_IXUSR = "_S_IEXEC"
const _UI16_MAX = "0xffffu"
const _UI32_MAX = "0xffffffffu"
const _UI64_MAX = "0xffffffffffffffffull"
const _UI8_MAX = "0xffu"
const _WAIT_CHILD = 0
const _WAIT_GRANDCHILD = 1
const _WConst_return = "_CONST_RETURN"
const _WIN32 = 1
const _WIN32_WINNT = 0xa00
const _X86_ = 1
const __ATOMIC_ACQUIRE = 2
const __ATOMIC_ACQ_REL = 4
const __ATOMIC_CONSUME = 1
const __ATOMIC_HLE_ACQUIRE = 65536
const __ATOMIC_HLE_RELEASE = 131072
const __ATOMIC_RELAXED = 0
const __ATOMIC_RELEASE = 3
const __ATOMIC_SEQ_CST = 5
const __BIGGEST_ALIGNMENT__ = 16
const __BYTE_ORDER__ = "__ORDER_LITTLE_ENDIAN__"
const __C89_NAMELESS = "__MINGW_EXTENSION"
const __CCGO__ = 1
const __CHAR_BIT__ = 8
const __CRTDECL = "__cdecl"
const __DBL_DECIMAL_DIG__ = 17
const __DBL_DIG__ = 15
const __DBL_HAS_DENORM__ = 1
const __DBL_HAS_INFINITY__ = 1
const __DBL_HAS_QUIET_NAN__ = 1
const __DBL_IS_IEC_60559__ = 2
const __DBL_MANT_DIG__ = 53
const __DBL_MAX_10_EXP__ = 308
const __DBL_MAX_EXP__ = 1024
const __DEC128_EPSILON__ = 1e-33
const __DEC128_MANT_DIG__ = 34
const __DEC128_MAX_EXP__ = 6145
const __DEC128_MAX__ = "9.999999999999999999999999999999999E6144"
const __DEC128_MIN__ = 1e-6143
const __DEC128_SUBNORMAL_MIN__ = 0.000000000000000000000000000000001e-6143
const __DEC32_EPSILON__ = 1e-6
const __DEC32_MANT_DIG__ = 7
const __DEC32_MAX_EXP__ = 97
const __DEC32_MAX__ = 9.999999e96
const __DEC32_MIN__ = 1e-95
const __DEC32_SUBNORMAL_MIN__ = 0.000001e-95
const __DEC64_EPSILON__ = 1e-15
const __DEC64_MANT_DIG__ = 16
const __DEC64_MAX_EXP__ = 385
const __DEC64_MAX__ = "9.999999999999999E384"
const __DEC64_MIN__ = 1e-383
const __DEC64_SUBNORMAL_MIN__ = 0.000000000000001e-383
const __DECIMAL_BID_FORMAT__ = 1
const __DECIMAL_DIG__ = 17
const __DEC_EVAL_METHOD__ = 2
const __FINITE_MATH_ONLY__ = 0
const __FLOAT_WORD_ORDER__ = "__ORDER_LITTLE_ENDIAN__"
const __FLT128_DECIMAL_DIG__ = 36
const __FLT128_DENORM_MIN__ = 6.47517511943802511092443895822764655e-4966
const __FLT128_DIG__ = 33
const __FLT128_EPSILON__ = 1.92592994438723585305597794258492732e-34
const __FLT128_HAS_DENORM__ = 1
const __FLT128_HAS_INFINITY__ = 1
const __FLT128_HAS_QUIET_NAN__ = 1
const __FLT128_IS_IEC_60559__ = 2
const __FLT128_MANT_DIG__ = 113
const __FLT128_MAX_10_EXP__ = 4932
const __FLT128_MAX_EXP__ = 16384
const __FLT128_MAX__ = "1.18973149535723176508575932662800702e+4932"
const __FLT128_MIN__ = 3.36210314311209350626267781732175260e-4932
const __FLT128_NORM_MAX__ = "1.18973149535723176508575932662800702e+4932"
const __FLT32X_DECIMAL_DIG__ = 17
const __FLT32X_DENORM_MIN__ = 4.94065645841246544176568792868221372e-324
const __FLT32X_DIG__ = 15
const __FLT32X_EPSILON__ = 2.22044604925031308084726333618164062e-16
const __FLT32X_HAS_DENORM__ = 1
const __FLT32X_HAS_INFINITY__ = 1
const __FLT32X_HAS_QUIET_NAN__ = 1
const __FLT32X_IS_IEC_60559__ = 2
const __FLT32X_MANT_DIG__ = 53
const __FLT32X_MAX_10_EXP__ = 308
const __FLT32X_MAX_EXP__ = 1024
const __FLT32X_MAX__ = 1.79769313486231570814527423731704357e+308
const __FLT32X_MIN__ = 2.22507385850720138309023271733240406e-308
const __FLT32X_NORM_MAX__ = 1.79769313486231570814527423731704357e+308
const __FLT32_DECIMAL_DIG__ = 9
const __FLT32_DENORM_MIN__ = 1.40129846432481707092372958328991613e-45
const __FLT32_DIG__ = 6
const __FLT32_EPSILON__ = 1.19209289550781250000000000000000000e-7
const __FLT32_HAS_DENORM__ = 1
const __FLT32_HAS_INFINITY__ = 1
const __FLT32_HAS_QUIET_NAN__ = 1
const __FLT32_IS_IEC_60559__ = 2
const __FLT32_MANT_DIG__ = 24
const __FLT32_MAX_10_EXP__ = 38
const __FLT32_MAX_EXP__ = 128
const __FLT32_MAX__ = 3.40282346638528859811704183484516925e+38
const __FLT32_MIN__ = 1.17549435082228750796873653722224568e-38
const __FLT32_NORM_MAX__ = 3.40282346638528859811704183484516925e+38
const __FLT64X_DECIMAL_DIG__ = 36
const __FLT64X_DENORM_MIN__ = 6.47517511943802511092443895822764655e-4966
const __FLT64X_DIG__ = 33
const __FLT64X_EPSILON__ = 1.92592994438723585305597794258492732e-34
const __FLT64X_HAS_DENORM__ = 1
const __FLT64X_HAS_INFINITY__ = 1
const __FLT64X_HAS_QUIET_NAN__ = 1
const __FLT64X_IS_IEC_60559__ = 2
const __FLT64X_MANT_DIG__ = 113
const __FLT64X_MAX_10_EXP__ = 4932
const __FLT64X_MAX_EXP__ = 16384
const __FLT64X_MAX__ = "1.18973149535723176508575932662800702e+4932"
const __FLT64X_MIN__ = 3.36210314311209350626267781732175260e-4932
const __FLT64X_NORM_MAX__ = "1.18973149535723176508575932662800702e+4932"
const __FLT64_DECIMAL_DIG__ = 17
const __FLT64_DENORM_MIN__ = 4.94065645841246544176568792868221372e-324
const __FLT64_DIG__ = 15
const __FLT64_EPSILON__ = 2.22044604925031308084726333618164062e-16
const __FLT64_HAS_DENORM__ = 1
const __FLT64_HAS_INFINITY__ = 1
const __FLT64_HAS_QUIET_NAN__ = 1
const __FLT64_IS_IEC_60559__ = 2
const __FLT64_MANT_DIG__ = 53
const __FLT64_MAX_10_EXP__ = 308
const __FLT64_MAX_EXP__ = 1024
const __FLT64_MAX__ = 1.79769313486231570814527423731704357e+308
const __FLT64_MIN__ = 2.22507385850720138309023271733240406e-308
const __FLT64_NORM_MAX__ = 1.79769313486231570814527423731704357e+308
const __FLT_DECIMAL_DIG__ = 9
const __FLT_DENORM_MIN__ = 1.40129846432481707092372958328991613e-45
const __FLT_DIG__ = 6
const __FLT_EPSILON__ = 1.19209289550781250000000000000000000e-7
const __FLT_EVAL_METHOD_TS_18661_3__ = 2
const __FLT_EVAL_METHOD__ = 2
const __FLT_HAS_DENORM__ = 1
const __FLT_HAS_INFINITY__ = 1
const __FLT_HAS_QUIET_NAN__ = 1
const __FLT_IS_IEC_60559__ = 2
const __FLT_MANT_DIG__ = 24
const __FLT_MAX_10_EXP__ = 38
const __FLT_MAX_EXP__ = 128
const __FLT_MAX__ = 3.40282346638528859811704183484516925e+38
const __FLT_MIN__ = 1.17549435082228750796873653722224568e-38
const __FLT_NORM_MAX__ = 3.40282346638528859811704183484516925e+38
const __FLT_RADIX__ = 2
const __FUNCTION__ = "__func__"
const __GCC_ASM_FLAG_OUTPUTS__ = 1
const __GCC_ATOMIC_BOOL_LOCK_FREE = 2
const __GCC_ATOMIC_CHAR16_T_LOCK_FREE = 2
const __GCC_ATOMIC_CHAR32_T_LOCK_FREE = 2
const __GCC_ATOMIC_CHAR_LOCK_FREE = 2
const __GCC_ATOMIC_INT_LOCK_FREE = 2
const __GCC_ATOMIC_LLONG_LOCK_FREE = 2
const __GCC_ATOMIC_LONG_LOCK_FREE = 2
const __GCC_ATOMIC_POINTER_LOCK_FREE = 2
const __GCC_ATOMIC_SHORT_LOCK_FREE = 2
const __GCC_ATOMIC_TEST_AND_SET_TRUEVAL = 1
const __GCC_ATOMIC_WCHAR_T_LOCK_FREE = 2
const __GCC_CONSTRUCTIVE_SIZE = 64
const __GCC_DESTRUCTIVE_SIZE = 64
const __GCC_HAVE_DWARF2_CFI_ASM = 1
const __GCC_HAVE_SYNC_COMPARE_AND_SWAP_1 = 1
const __GCC_HAVE_SYNC_COMPARE_AND_SWAP_2 = 1
const __GCC_HAVE_SYNC_COMPARE_AND_SWAP_4 = 1
const __GCC_HAVE_SYNC_COMPARE_AND_SWAP_8 = 1
const __GCC_IEC_559 = 2
const __GCC_IEC_559_COMPLEX = 2
const __GNUC_EXECUTION_CHARSET_NAME = "UTF-8"
const __GNUC_MINOR__ = 0
const __GNUC_PATCHLEVEL__ = 0
const __GNUC_STDC_INLINE__ = 1
const __GNUC_WIDE_EXECUTION_CHARSET_NAME = "UTF-16LE"
const __GNUC__ = 12
const __GNU_EXTENSION = "__MINGW_EXTENSION"
const __GOT_SECURE_LIB__ = "__STDC_SECURE_LIB__"
const __GXX_ABI_VERSION = 1017
const __GXX_MERGED_TYPEINFO_NAMES = 0
const __GXX_TYPEINFO_EQUALITY_INLINE = 0
const __HAVE_SPECULATION_SAFE_VALUE = 1
const __ILP32__ = 1
const __INT16_MAX__ = 0x7fff
const __INT32_MAX__ = 0x7fffffff
const __INT32_TYPE__ = "int"
const __INT64_MAX__ = 0x7fffffffffffffff
const __INT8_MAX__ = 0x7f
const __INTMAX_MAX__ = 0x7fffffffffffffff
const __INTMAX_WIDTH__ = 64
const __INTPTR_MAX__ = 0x7fffffff
const __INTPTR_TYPE__ = "int"
const __INTPTR_WIDTH__ = 32
const __INT_FAST16_MAX__ = 0x7fff
const __INT_FAST16_WIDTH__ = 16
const __INT_FAST32_MAX__ = 0x7fffffff
const __INT_FAST32_TYPE__ = "int"
const __INT_FAST32_WIDTH__ = 32
const __INT_FAST64_MAX__ = 0x7fffffffffffffff
const __INT_FAST64_WIDTH__ = 64
const __INT_FAST8_MAX__ = 0x7f
const __INT_FAST8_WIDTH__ = 8
const __INT_LEAST16_MAX__ = 0x7fff
const __INT_LEAST16_WIDTH__ = 16
const __INT_LEAST32_MAX__ = 0x7fffffff
const __INT_LEAST32_TYPE__ = "int"
const __INT_LEAST32_WIDTH__ = 32
const __INT_LEAST64_MAX__ = 0x7fffffffffffffff
const __INT_LEAST64_WIDTH__ = 64
const __INT_LEAST8_MAX__ = 0x7f
const __INT_LEAST8_WIDTH__ = 8
const __INT_MAX__ = 0x7fffffff
const __INT_WIDTH__ = 32
const __LAHF_SAHF__ = 1
const __LDBL_DECIMAL_DIG__ = 17
const __LDBL_DENORM_MIN__ = 4.94065645841246544176568792868221372e-324
const __LDBL_DIG__ = 15
const __LDBL_EPSILON__ = 2.22044604925031308084726333618164062e-16
const __LDBL_HAS_DENORM__ = 1
const __LDBL_HAS_INFINITY__ = 1
const __LDBL_HAS_QUIET_NAN__ = 1
const __LDBL_IS_IEC_60559__ = 2
const __LDBL_MANT_DIG__ = 53
const __LDBL_MAX_10_EXP__ = 308
const __LDBL_MAX_EXP__ = 1024
const __LDBL_MAX__ = 1.79769313486231570814527423731704357e+308
const __LDBL_MIN__ = 2.22507385850720138309023271733240406e-308
const __LDBL_NORM_MAX__ = 1.79769313486231570814527423731704357e+308
const __LONG32 = "long"
const __LONG_DOUBLE_64__ = 1
const __LONG_LONG_MAX__ = 0x7fffffffffffffff
const __LONG_LONG_WIDTH__ = 64
const __LONG_MAX__ = 0x7fffffff
const __LONG_WIDTH__ = 32
const __MINGW32_MAJOR_VERSION = 3
const __MINGW32_MINOR_VERSION = 11
const __MINGW32__ = 1
const __MINGW64_VERSION_BUGFIX = 0
const __MINGW64_VERSION_MAJOR = 10
const __MINGW64_VERSION_MINOR = 0
const __MINGW64_VERSION_RC = 0
const __MINGW64_VERSION_STATE = "alpha"
const __MINGW_DEBUGBREAK_IMPL = 1
const __MINGW_FORTIFY_LEVEL = 0
const __MINGW_FORTIFY_VA_ARG = 0
const __MINGW_HAVE_ANSI_C99_PRINTF = 1
const __MINGW_HAVE_ANSI_C99_SCANF = 1
const __MINGW_HAVE_WIDE_C99_PRINTF = 1
const __MINGW_HAVE_WIDE_C99_SCANF = 1
const __MINGW_MSVC2005_DEPREC_STR = "This POSIX function is deprecated beginning in Visual C++ 2005, use _CRT_NONSTDC_NO_DEPRECATE to disable deprecation"
const __MINGW_SEC_WARN_STR = "This function or variable may be unsafe, use _CRT_SECURE_NO_WARNINGS to disable deprecation"
const __MSVCRT_VERSION__ = 0x700
const __MSVCRT__ = 1
const __NO_INLINE__ = 1
const __ORDER_BIG_ENDIAN__ = 4321
const __ORDER_LITTLE_ENDIAN__ = 1234
const __ORDER_PDP_ENDIAN__ = 3412
const __PRAGMA_REDEFINE_EXTNAME = 1
const __PRETTY_FUNCTION__ = "__func__"
const __PTRDIFF_MAX__ = 0x7fffffff
const __PTRDIFF_TYPE__ = "int"
const __PTRDIFF_WIDTH__ = 32
const __SCHAR_MAX__ = 0x7f
const __SCHAR_WIDTH__ = 8
const __SEG_FS = 1
const __SEG_GS = 1
const __SHRT_MAX__ = 0x7fff
const __SHRT_WIDTH__ = 16
const __SIG_ATOMIC_MAX__ = 0x7fffffff
const __SIG_ATOMIC_TYPE__ = "int"
const __SIG_ATOMIC_WIDTH__ = 32
const __SIZEOF_DOUBLE__ = 8
const __SIZEOF_FLOAT128__ = 16
const __SIZEOF_FLOAT80__ = 12
const __SIZEOF_FLOAT__ = 4
const __SIZEOF_INT__ = 4
const __SIZEOF_LONG_DOUBLE__ = 8
const __SIZEOF_LONG_LONG__ = 8
const __SIZEOF_LONG__ = 4
const __SIZEOF_POINTER__ = 4
const __SIZEOF_PTRDIFF_T__ = 4
const __SIZEOF_SHORT__ = 2
const __SIZEOF_SIZE_T__ = 4
const __SIZEOF_WCHAR_T__ = 2
const __SIZEOF_WINT_T__ = 2
const __SIZE_MAX__ = 0xffffffff
const __SIZE_WIDTH__ = 32
const __STDC_HOSTED__ = 1
const __STDC_SECURE_LIB__ = 200411
const __STDC_UTF_16__ = 1
const __STDC_UTF_32__ = 1
const __STDC_VERSION__ = 201710
const __STDC__ = 1
const __UINT16_MAX__ = 0xffff
const __UINT32_MAX__ = 0xffffffff
const __UINT64_MAX__ = "0xffffffffffffffffU"
const __UINT8_MAX__ = 0xff
const __UINTMAX_MAX__ = "0xffffffffffffffffU"
const __UINTPTR_MAX__ = 0xffffffff
const __UINT_FAST16_MAX__ = 0xffff
const __UINT_FAST32_MAX__ = 0xffffffff
const __UINT_FAST64_MAX__ = "0xffffffffffffffffU"
const __UINT_FAST8_MAX__ = 0xff
const __UINT_LEAST16_MAX__ = 0xffff
const __UINT_LEAST32_MAX__ = 0xffffffff
const __UINT_LEAST64_MAX__ = "0xffffffffffffffffU"
const __UINT_LEAST8_MAX__ = 0xff
const __USER_LABEL_PREFIX__ = "_"
const __USE_MINGW_ANSI_STDIO = 1
const __VERSION__ = "12-win32"
const __WCHAR_MAX__ = 0xffff
const __WCHAR_MIN__ = 0
const __WCHAR_WIDTH__ = 16
const __WIN32 = 1
const __WIN32__ = 1
const __WINNT = 1
const __WINNT__ = 1
const __WINT_MAX__ = 0xffff
const __WINT_MIN__ = 0
const __WINT_WIDTH__ = 16
const __clockid_t_defined = 1
const __code_model_32__ = 1
const __i386 = 1
const __i386__ = 1
const __i686 = 1
const __i686__ = 1
const __int16 = "short"
const __int32 = "int"
const __int8 = "char"
const __mingw_bos_ovr = "__mingw_ovr"
const __pentiumpro = 1
const __pentiumpro__ = 1
const __stat64 = "_stat64"
const _finddata_t = "_finddata32_t"
const _finddatai64_t = "_finddata32i64_t"
const _findfirst = "_findfirst32"
const _findfirsti64 = "_findfirst32i64"
const _findnext = "_findnext32"
const _findnexti64 = "_findnext32i64"
const _fstat = "_fstat32"
const _fstat32i64 = "_fstati64"
const _ftime = "_ftime32"
const _ftime_s = "_ftime32_s"
const _inline = "__inline"
const _stat = "_stat32"
const _stat32i64 = "_stati64"
const _timeb = "__timeb32"
const _wfinddata_t = "_wfinddata32_t"
const _wfinddatai64_t = "_wfinddata32i64_t"
const _wfindfirst = "_wfindfirst32"
const _wfindfirst32i64 = "_wfindfirsti64"
const _wfindnext = "_wfindnext32"
const _wfindnext32i64 = "_wfindnexti64"
const _wstat = "_wstat32"
const _wstat32i64 = "_wstati64"
const fstat = "_fstat32i64"
const fstat64 = "_fstat64"
const ftruncate = "ftruncate64"
const i386 = 1
const lseek = "lseek64"
const stat1 = "_stat32i64"
const stat64 = "_stat64"
const strcasecmp = "_stricmp"
const strncasecmp = "_strnicmp"
const wcswcs = "wcsstr"

type __builtin_va_list = uintptr

type __predefined_size_t = uint32

type __predefined_wchar_t = uint16

type __predefined_ptrdiff_t = int32

type __gnuc_va_list = uintptr

type va_list = uintptr

type sqlite_int64 = int64

type sqlite_uint64 = uint64

type sqlite3_int64 = int64

type sqlite3_uint64 = uint64

type sqlite3_callback = uintptr

type sqlite3_file = struct {
	pMethods uintptr
}

type sqlite3_file1 = struct {
	pMethods uintptr
}

type sqlite3_io_methods = struct {
	iVersion               int32
	xClose                 uintptr
	xRead                  uintptr
	xWrite                 uintptr
	xTruncate              uintptr
	xSync                  uintptr
	xFileSize              uintptr
	xLock                  uintptr
	xUnlock                uintptr
	xCheckReservedLock     uintptr
	xFileControl           uintptr
	xSectorSize            uintptr
	xDeviceCharacteristics uintptr
	xShmMap                uintptr
	xShmLock               uintptr
	xShmBarrier            uintptr
	xShmUnmap              uintptr
	xFetch                 uintptr
	xUnfetch               uintptr
}

type sqlite3_io_methods1 = struct {
	iVersion               int32
	xClose                 uintptr
	xRead                  uintptr
	xWrite                 uintptr
	xTruncate              uintptr
	xSync                  uintptr
	xFileSize              uintptr
	xLock                  uintptr
	xUnlock                uintptr
	xCheckReservedLock     uintptr
	xFileControl           uintptr
	xSectorSize            uintptr
	xDeviceCharacteristics uintptr
	xShmMap                uintptr
	xShmLock               uintptr
	xShmBarrier            uintptr
	xShmUnmap              uintptr
	xFetch                 uintptr
	xUnfetch               uintptr
}

type sqlite3_filename = uintptr

type sqlite3_vfs = struct {
	iVersion          int32
	szOsFile          int32
	mxPathname        int32
	pNext             uintptr
	zName             uintptr
	pAppData          uintptr
	xOpen             uintptr
	xDelete           uintptr
	xAccess           uintptr
	xFullPathname     uintptr
	xDlOpen           uintptr
	xDlError          uintptr
	xDlSym            uintptr
	xDlClose          uintptr
	xRandomness       uintptr
	xSleep            uintptr
	xCurrentTime      uintptr
	xGetLastError     uintptr
	xCurrentTimeInt64 uintptr
	xSetSystemCall    uintptr
	xGetSystemCall    uintptr
	xNextSystemCall   uintptr
}

type sqlite3_syscall_ptr = uintptr

type sqlite3_vfs1 = struct {
	iVersion          int32
	szOsFile          int32
	mxPathname        int32
	pNext             uintptr
	zName             uintptr
	pAppData          uintptr
	xOpen             uintptr
	xDelete           uintptr
	xAccess           uintptr
	xFullPathname     uintptr
	xDlOpen           uintptr
	xDlError          uintptr
	xDlSym            uintptr
	xDlClose          uintptr
	xRandomness       uintptr
	xSleep            uintptr
	xCurrentTime      uintptr
	xGetLastError     uintptr
	xCurrentTimeInt64 uintptr
	xSetSystemCall    uintptr
	xGetSystemCall    uintptr
	xNextSystemCall   uintptr
}

type sqlite3_mem_methods = struct {
	xMalloc   uintptr
	xFree     uintptr
	xRealloc  uintptr
	xSize     uintptr
	xRoundup  uintptr
	xInit     uintptr
	xShutdown uintptr
	pAppData  uintptr
}

type sqlite3_mem_methods1 = struct {
	xMalloc   uintptr
	xFree     uintptr
	xRealloc  uintptr
	xSize     uintptr
	xRoundup  uintptr
	xInit     uintptr
	xShutdown uintptr
	pAppData  uintptr
}

type sqlite3_destructor_type = uintptr

type sqlite3_vtab = struct {
	pModule uintptr
	nRef    int32
	zErrMsg uintptr
}

type sqlite3_index_info = struct {
	__ccgo_align     [0]uint32
	nConstraint      int32
	aConstraint      uintptr
	nOrderBy         int32
	aOrderBy         uintptr
	aConstraintUsage uintptr
	idxNum           int32
	idxStr           uintptr
	needToFreeIdxStr int32
	orderByConsumed  int32
	__ccgo_align9    [4]byte
	estimatedCost    float64
	estimatedRows    sqlite3_int64
	idxFlags         int32
	__ccgo_align12   [4]byte
	colUsed          sqlite3_uint64
}

type sqlite3_vtab_cursor = struct {
	pVtab uintptr
}

type sqlite3_module = struct {
	iVersion      int32
	xCreate       uintptr
	xConnect      uintptr
	xBestIndex    uintptr
	xDisconnect   uintptr
	xDestroy      uintptr
	xOpen         uintptr
	xClose        uintptr
	xFilter       uintptr
	xNext         uintptr
	xEof          uintptr
	xColumn       uintptr
	xRowid        uintptr
	xUpdate       uintptr
	xBegin        uintptr
	xSync         uintptr
	xCommit       uintptr
	xRollback     uintptr
	xFindFunction uintptr
	xRename       uintptr
	xSavepoint    uintptr
	xRelease      uintptr
	xRollbackTo   uintptr
	xShadowName   uintptr
	xIntegrity    uintptr
}

type sqlite3_module1 = struct {
	iVersion      int32
	xCreate       uintptr
	xConnect      uintptr
	xBestIndex    uintptr
	xDisconnect   uintptr
	xDestroy      uintptr
	xOpen         uintptr
	xClose        uintptr
	xFilter       uintptr
	xNext         uintptr
	xEof          uintptr
	xColumn       uintptr
	xRowid        uintptr
	xUpdate       uintptr
	xBegin        uintptr
	xSync         uintptr
	xCommit       uintptr
	xRollback     uintptr
	xFindFunction uintptr
	xRename       uintptr
	xSavepoint    uintptr
	xRelease      uintptr
	xRollbackTo   uintptr
	xShadowName   uintptr
	xIntegrity    uintptr
}

type sqlite3_index_info1 = struct {
	__ccgo_align     [0]uint32
	nConstraint      int32
	aConstraint      uintptr
	nOrderBy         int32
	aOrderBy         uintptr
	aConstraintUsage uintptr
	idxNum           int32
	idxStr           uintptr
	needToFreeIdxStr int32
	orderByConsumed  int32
	__ccgo_align9    [4]byte
	estimatedCost    float64
	estimatedRows    sqlite3_int64
	idxFlags         int32
	__ccgo_align12   [4]byte
	colUsed          sqlite3_uint64
}

type sqlite3_vtab1 = struct {
	pModule uintptr
	nRef    int32
	zErrMsg uintptr
}

type sqlite3_vtab_cursor1 = struct {
	pVtab uintptr
}

type sqlite3_mutex_methods = struct {
	xMutexInit    uintptr
	xMutexEnd     uintptr
	xMutexAlloc   uintptr
	xMutexFree    uintptr
	xMutexEnter   uintptr
	xMutexTry     uintptr
	xMutexLeave   uintptr
	xMutexHeld    uintptr
	xMutexNotheld uintptr
}

type sqlite3_mutex_methods1 = struct {
	xMutexInit    uintptr
	xMutexEnd     uintptr
	xMutexAlloc   uintptr
	xMutexFree    uintptr
	xMutexEnter   uintptr
	xMutexTry     uintptr
	xMutexLeave   uintptr
	xMutexHeld    uintptr
	xMutexNotheld uintptr
}

type sqlite3_pcache_page = struct {
	pBuf   uintptr
	pExtra uintptr
}

type sqlite3_pcache_page1 = struct {
	pBuf   uintptr
	pExtra uintptr
}

type sqlite3_pcache_methods2 = struct {
	iVersion   int32
	pArg       uintptr
	xInit      uintptr
	xShutdown  uintptr
	xCreate    uintptr
	xCachesize uintptr
	xPagecount uintptr
	xFetch     uintptr
	xUnpin     uintptr
	xRekey     uintptr
	xTruncate  uintptr
	xDestroy   uintptr
	xShrink    uintptr
}

type sqlite3_pcache_methods21 = struct {
	iVersion   int32
	pArg       uintptr
	xInit      uintptr
	xShutdown  uintptr
	xCreate    uintptr
	xCachesize uintptr
	xPagecount uintptr
	xFetch     uintptr
	xUnpin     uintptr
	xRekey     uintptr
	xTruncate  uintptr
	xDestroy   uintptr
	xShrink    uintptr
}

type sqlite3_pcache_methods = struct {
	pArg       uintptr
	xInit      uintptr
	xShutdown  uintptr
	xCreate    uintptr
	xCachesize uintptr
	xPagecount uintptr
	xFetch     uintptr
	xUnpin     uintptr
	xRekey     uintptr
	xTruncate  uintptr
	xDestroy   uintptr
}

type sqlite3_pcache_methods1 = struct {
	pArg       uintptr
	xInit      uintptr
	xShutdown  uintptr
	xCreate    uintptr
	xCachesize uintptr
	xPagecount uintptr
	xFetch     uintptr
	xUnpin     uintptr
	xRekey     uintptr
	xTruncate  uintptr
	xDestroy   uintptr
}

type sqlite3_snapshot = struct {
	hidden [48]uint8
}

type sqlite3_rtree_geometry = struct {
	pContext uintptr
	nParam   int32
	aParam   uintptr
	pUser    uintptr
	xDelUser uintptr
}

type sqlite3_rtree_query_info = struct {
	__ccgo_align  [0]uint32
	pContext      uintptr
	nParam        int32
	aParam        uintptr
	pUser         uintptr
	xDelUser      uintptr
	aCoord        uintptr
	anQueue       uintptr
	nCoord        int32
	iLevel        int32
	mxLevel       int32
	iRowid        sqlite3_int64
	rParentScore  sqlite3_rtree_dbl
	eParentWithin int32
	eWithin       int32
	rScore        sqlite3_rtree_dbl
	apSqlParam    uintptr
	__ccgo_pad16  [4]byte
}

type sqlite3_rtree_dbl = float64

type sqlite3_rtree_geometry1 = struct {
	pContext uintptr
	nParam   int32
	aParam   uintptr
	pUser    uintptr
	xDelUser uintptr
}

type sqlite3_rtree_query_info1 = struct {
	__ccgo_align  [0]uint32
	pContext      uintptr
	nParam        int32
	aParam        uintptr
	pUser         uintptr
	xDelUser      uintptr
	aCoord        uintptr
	anQueue       uintptr
	nCoord        int32
	iLevel        int32
	mxLevel       int32
	iRowid        sqlite3_int64
	rParentScore  sqlite3_rtree_dbl
	eParentWithin int32
	eWithin       int32
	rScore        sqlite3_rtree_dbl
	apSqlParam    uintptr
	__ccgo_pad16  [4]byte
}

type Fts5ExtensionApi = struct {
	iVersion           int32
	xUserData          uintptr
	xColumnCount       uintptr
	xRowCount          uintptr
	xColumnTotalSize   uintptr
	xTokenize          uintptr
	xPhraseCount       uintptr
	xPhraseSize        uintptr
	xInstCount         uintptr
	xInst              uintptr
	xRowid             uintptr
	xColumnText        uintptr
	xColumnSize        uintptr
	xQueryPhrase       uintptr
	xSetAuxdata        uintptr
	xGetAuxdata        uintptr
	xPhraseFirst       uintptr
	xPhraseNext        uintptr
	xPhraseFirstColumn uintptr
	xPhraseNextColumn  uintptr
	xQueryToken        uintptr
	xInstToken         uintptr
}

type Fts5PhraseIter = struct {
	a uintptr
	b uintptr
}

type fts5_extension_function = uintptr

type Fts5PhraseIter1 = struct {
	a uintptr
	b uintptr
}

type Fts5ExtensionApi1 = struct {
	iVersion           int32
	xUserData          uintptr
	xColumnCount       uintptr
	xRowCount          uintptr
	xColumnTotalSize   uintptr
	xTokenize          uintptr
	xPhraseCount       uintptr
	xPhraseSize        uintptr
	xInstCount         uintptr
	xInst              uintptr
	xRowid             uintptr
	xColumnText        uintptr
	xColumnSize        uintptr
	xQueryPhrase       uintptr
	xSetAuxdata        uintptr
	xGetAuxdata        uintptr
	xPhraseFirst       uintptr
	xPhraseNext        uintptr
	xPhraseFirstColumn uintptr
	xPhraseNextColumn  uintptr
	xQueryToken        uintptr
	xInstToken         uintptr
}

type fts5_tokenizer = struct {
	xCreate   uintptr
	xDelete   uintptr
	xTokenize uintptr
}

type fts5_tokenizer1 = struct {
	xCreate   uintptr
	xDelete   uintptr
	xTokenize uintptr
}

type fts5_api = struct {
	iVersion         int32
	xCreateTokenizer uintptr
	xFindTokenizer   uintptr
	xCreateFunction  uintptr
}

type fts5_api1 = struct {
	iVersion         int32
	xCreateTokenizer uintptr
	xFindTokenizer   uintptr
	xCreateFunction  uintptr
}

type size_t = uint32

type ssize_t = int32

type rsize_t = uint32

type intptr_t = int32

type uintptr_t = uint32

type ptrdiff_t = int32

type wchar_t = uint16

type wint_t = uint16

type wctype_t = uint16

type errno_t = int32

type __time32_t = int32

type __time64_t = int64

type time_t = int32

type threadlocaleinfostruct = struct {
	refcount      int32
	lc_codepage   uint32
	lc_collate_cp uint32
	lc_handle     [6]uint32
	lc_id         [6]LC_ID
	lc_category   [6]struct {
		locale    uintptr
		wlocale   uintptr
		refcount  uintptr
		wrefcount uintptr
	}
	lc_clike            int32
	mb_cur_max          int32
	lconv_intl_refcount uintptr
	lconv_num_refcount  uintptr
	lconv_mon_refcount  uintptr
	lconv               uintptr
	ctype1_refcount     uintptr
	ctype1              uintptr
	pctype              uintptr
	pclmap              uintptr
	pcumap              uintptr
	lc_time_curr        uintptr
}

type pthreadlocinfo = uintptr

type pthreadmbcinfo = uintptr

type _locale_tstruct = struct {
	locinfo pthreadlocinfo
	mbcinfo pthreadmbcinfo
}

type localeinfo_struct = _locale_tstruct

type _locale_t = uintptr

type LC_ID = struct {
	wLanguage uint16
	wCountry  uint16
	wCodePage uint16
}

type tagLC_ID = LC_ID

type LPLC_ID = uintptr

type threadlocinfo = struct {
	refcount      int32
	lc_codepage   uint32
	lc_collate_cp uint32
	lc_handle     [6]uint32
	lc_id         [6]LC_ID
	lc_category   [6]struct {
		locale    uintptr
		wlocale   uintptr
		refcount  uintptr
		wrefcount uintptr
	}
	lc_clike            int32
	mb_cur_max          int32
	lconv_intl_refcount uintptr
	lconv_num_refcount  uintptr
	lconv_mon_refcount  uintptr
	lconv               uintptr
	ctype1_refcount     uintptr
	ctype1              uintptr
	pctype              uintptr
	pclmap              uintptr
	pcumap              uintptr
	lc_time_curr        uintptr
}

type _ino_t = uint16

type ino_t = uint16

type _dev_t = uint32

type dev_t = uint32

type _pid_t = int32

type pid_t = int32

type _mode_t = uint16

type mode_t = uint16

type _off_t = int32

type off32_t = int32

type _off64_t = int64

type off64_t = int64

type off_t = int64

type useconds_t = uint32

type timespec = struct {
	tv_sec  time_t
	tv_nsec int32
}

type itimerspec = struct {
	it_interval timespec
	it_value    timespec
}

type _sigset_t = uint32

type _fsize_t = uint32

type _finddata32_t = struct {
	attrib      uint32
	time_create __time32_t
	time_access __time32_t
	time_write  __time32_t
	size        _fsize_t
	name        [260]int8
}

type _finddata32i64_t = struct {
	__ccgo_align [0]uint32
	attrib       uint32
	time_create  __time32_t
	time_access  __time32_t
	time_write   __time32_t
	size         int64
	name         [260]int8
	__ccgo_pad6  [4]byte
}

type _finddata64i32_t = struct {
	__ccgo_align  [0]uint32
	attrib        uint32
	__ccgo_align1 [4]byte
	time_create   __time64_t
	time_access   __time64_t
	time_write    __time64_t
	size          _fsize_t
	name          [260]int8
}

type __finddata64_t = struct {
	__ccgo_align  [0]uint32
	attrib        uint32
	__ccgo_align1 [4]byte
	time_create   __time64_t
	time_access   __time64_t
	time_write    __time64_t
	size          int64
	name          [260]int8
	__ccgo_pad6   [4]byte
}

type _wfinddata32_t = struct {
	attrib      uint32
	time_create __time32_t
	time_access __time32_t
	time_write  __time32_t
	size        _fsize_t
	name        [260]wchar_t
}

type _wfinddata32i64_t = struct {
	__ccgo_align [0]uint32
	attrib       uint32
	time_create  __time32_t
	time_access  __time32_t
	time_write   __time32_t
	size         int64
	name         [260]wchar_t
}

type _wfinddata64i32_t = struct {
	__ccgo_align  [0]uint32
	attrib        uint32
	__ccgo_align1 [4]byte
	time_create   __time64_t
	time_access   __time64_t
	time_write    __time64_t
	size          _fsize_t
	name          [260]wchar_t
	__ccgo_pad6   [4]byte
}

type _wfinddata64_t = struct {
	__ccgo_align  [0]uint32
	attrib        uint32
	__ccgo_align1 [4]byte
	time_create   __time64_t
	time_access   __time64_t
	time_write    __time64_t
	size          int64
	name          [260]wchar_t
}

type _stat32 = struct {
	st_dev   _dev_t
	st_ino   _ino_t
	st_mode  uint16
	st_nlink int16
	st_uid   int16
	st_gid   int16
	st_rdev  _dev_t
	st_size  _off_t
	st_atime __time32_t
	st_mtime __time32_t
	st_ctime __time32_t
}

type stat = struct {
	st_dev   _dev_t
	st_ino   _ino_t
	st_mode  uint16
	st_nlink int16
	st_uid   int16
	st_gid   int16
	st_rdev  _dev_t
	st_size  _off_t
	st_atime time_t
	st_mtime time_t
	st_ctime time_t
}

type _stati64 = struct {
	__ccgo_align  [0]uint32
	st_dev        _dev_t
	st_ino        _ino_t
	st_mode       uint16
	st_nlink      int16
	st_uid        int16
	st_gid        int16
	st_rdev       _dev_t
	__ccgo_align7 [4]byte
	st_size       int64
	st_atime      __time32_t
	st_mtime      __time32_t
	st_ctime      __time32_t
	__ccgo_pad11  [4]byte
}

type _stat64i32 = struct {
	__ccgo_align [0]uint32
	st_dev       _dev_t
	st_ino       _ino_t
	st_mode      uint16
	st_nlink     int16
	st_uid       int16
	st_gid       int16
	st_rdev      _dev_t
	st_size      _off_t
	st_atime     __time64_t
	st_mtime     __time64_t
	st_ctime     __time64_t
}

type _stat64 = struct {
	__ccgo_align  [0]uint32
	st_dev        _dev_t
	st_ino        _ino_t
	st_mode       uint16
	st_nlink      int16
	st_uid        int16
	st_gid        int16
	st_rdev       _dev_t
	__ccgo_align7 [4]byte
	st_size       int64
	st_atime      __time64_t
	st_mtime      __time64_t
	st_ctime      __time64_t
}

type _PVFV = uintptr

type _PIFV = uintptr

type _PVFI = uintptr

type _onexit_table_t = struct {
	_first uintptr
	_last  uintptr
	_end   uintptr
}

type _onexit_t = uintptr

type _beginthread_proc_type = uintptr

type _beginthreadex_proc_type = uintptr

type _tls_callback_type = uintptr

type __timeb32 = struct {
	time     __time32_t
	millitm  uint16
	timezone int16
	dstflag  int16
}

type timeb = struct {
	time     time_t
	millitm  uint16
	timezone int16
	dstflag  int16
}

type __timeb64 = struct {
	__ccgo_align [0]uint32
	time         __time64_t
	millitm      uint16
	timezone     int16
	dstflag      int16
	__ccgo_pad4  [2]byte
}

type _timespec32 = struct {
	tv_sec  __time32_t
	tv_nsec int32
}

type _timespec64 = struct {
	__ccgo_align [0]uint32
	tv_sec       __time64_t
	tv_nsec      int32
	__ccgo_pad2  [4]byte
}

type clock_t = int32

type tm = struct {
	tm_sec   int32
	tm_min   int32
	tm_hour  int32
	tm_mday  int32
	tm_mon   int32
	tm_year  int32
	tm_wday  int32
	tm_yday  int32
	tm_isdst int32
}

type timeval = struct {
	tv_sec  int32
	tv_usec int32
}

type timezone = struct {
	tz_minuteswest int32
	tz_dsttime     int32
}

type clockid_t = int32

/* Posix thread extensions.  */

/* Extension defined as by report VC 10+ defines error-numbers.  */

/* Defined as WSAETIMEDOUT.  */

/**
 * This file has no copyright assigned and is placed in the Public Domain.
 * This file is part of the mingw-w64 runtime package.
 * No warranty is given; refer to the file DISCLAIMER.PD within this package.
 */
/**
 * This file has no copyright assigned and is placed in the Public Domain.
 * This file is part of the mingw-w64 runtime package.
 * No warranty is given; refer to the file DISCLAIMER.PD within this package.
 */

/**
 * This file has no copyright assigned and is placed in the Public Domain.
 * This file is part of the mingw-w64 runtime package.
 * No warranty is given; refer to the file DISCLAIMER.PD within this package.
 */

// #define hook __builtin_printf("TRC %s:%i:\n", __func__, __LINE__);

/*
** Size of the write buffer used by journal files in bytes.
 */

/*
** The maximum pathname length supported by this VFS.
 */

// C documentation
//
//	/*
//	** When using this VFS, the sqlite3_file* handles that SQLite uses are
//	** actually pointers to instances of type VFSFile.
//	*/
type VFSFile = struct {
	__ccgo_align  [0]uint32
	base          sqlite3_file
	fsFile        uintptr
	fd            int32
	aBuffer       uintptr
	nBuffer       int32
	__ccgo_align5 [4]byte
	iBufferOfst   sqlite3_int64
}

type VFSFile1 = struct {
	__ccgo_align  [0]uint32
	base          sqlite3_file
	fsFile        uintptr
	fd            int32
	aBuffer       uintptr
	nBuffer       int32
	__ccgo_align5 [4]byte
	iBufferOfst   sqlite3_int64
}

// C documentation
//
//	/*
//	** Write directly to the file passed as the first argument. Even if the
//	** file has a write-buffer (VFSFile.aBuffer), ignore it.
//	*/
func vfsDirectWrite(tls *libc.TLS, p uintptr, zBuf uintptr, iAmt int32, iOfst sqlite_int64) (r int32) {
	panic("not implemeneted")
}

var __func__ = [15]int8{'v', 'f', 's', 'D', 'i', 'r', 'e', 'c', 't', 'W', 'r', 'i', 't', 'e'} /* Return value from write() */

// C documentation
//
//	/*
//	** Flush the contents of the VFSFile.aBuffer buffer to disk. This is a
//	** no-op if this particular file does not have a buffer (i.e. it is not
//	** a journal file) or if the buffer is currently empty.
//	*/
func vfsFlushBuffer(tls *libc.TLS, p uintptr) (r int32) {
	bp := tls.Alloc(32)
	defer tls.Free(32)
	var rc int32
	_ = rc
	libc.X__builtin_printf(tls, __ccgo_ts, libc.VaList(bp+8, uintptr(unsafe.Pointer(&__func__1)), int32(198)))
	libc.Xabort(tls)
	rc = SQLITE_OK
	if (*VFSFile)(unsafe.Pointer(p)).nBuffer != 0 {
		rc = vfsDirectWrite(tls, p, (*VFSFile)(unsafe.Pointer(p)).aBuffer, (*VFSFile)(unsafe.Pointer(p)).nBuffer, (*VFSFile)(unsafe.Pointer(p)).iBufferOfst)
		(*VFSFile)(unsafe.Pointer(p)).nBuffer = 0
	}
	return rc
}

var __func__1 = [15]int8{'v', 'f', 's', 'F', 'l', 'u', 's', 'h', 'B', 'u', 'f', 'f', 'e', 'r'}

// C documentation
//
//	/*
//	** Write data to a crash-file.
//	*/
func vfsWrite(tls *libc.TLS, pFile uintptr, zBuf uintptr, iAmt int32, iOfst sqlite_int64) (r int32) {
	panic("not implemeneted")
}

var __func__2 = [9]int8{'v', 'f', 's', 'W', 'r', 'i', 't', 'e'}

// C documentation
//
//	/*
//	** Truncate a file. This is a no-op for this VFS (see header comments at
//	** the top of the file).
//	*/
func vfsTruncate(tls *libc.TLS, pFile uintptr, size sqlite_int64) (r int32) {
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** Sync the contents of the file to the persistent media.
//	*/
func vfsSync(tls *libc.TLS, pFile uintptr, flags int32) (r int32) {
	panic("not implemeneted")
}

var __func__3 = [8]int8{'v', 'f', 's', 'S', 'y', 'n', 'c'}

// C documentation
//
//	/*
//	** Locking functions. The xLock() and xUnlock() methods are both no-ops.
//	** The xCheckReservedLock() always indicates that no other process holds
//	** a reserved lock on the database file. This ensures that if a hot-journal
//	** file is found in the file-system it is rolled back.
//	*/
func vfsLock(tls *libc.TLS, pFile uintptr, eLock int32) (r int32) {
	return SQLITE_OK
}

func vfsUnlock(tls *libc.TLS, pFile uintptr, eLock int32) (r int32) {
	return SQLITE_OK
}

func vfsCheckReservedLock(tls *libc.TLS, pFile uintptr, pResOut uintptr) (r int32) {
	*(*int32)(unsafe.Pointer(pResOut)) = 0
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** No xFileControl() verbs are implemented by this VFS.
//	*/
func vfsFileControl(tls *libc.TLS, pFile uintptr, op int32, pArg uintptr) (r int32) {
	return int32(SQLITE_NOTFOUND)
}

// C documentation
//
//	/*
//	** The xSectorSize() and xDeviceCharacteristics() methods. These two
//	** may return special values allowing SQLite to optimize file-system
//	** access to some extent. But it is also safe to simply return 0.
//	*/
func vfsSectorSize(tls *libc.TLS, pFile uintptr) (r int32) {
	return 0
}

func vfsDeviceCharacteristics(tls *libc.TLS, pFile uintptr) (r int32) {
	return 0
}

// C documentation
//
//	/*
//	** Delete the file identified by argument zPath. If the dirSync parameter
//	** is non-zero, then ensure the file-system modification to delete the
//	** file has been synced to disk before returning.
//	*/
func vfsDelete(tls *libc.TLS, pVfs uintptr, zPath uintptr, dirSync int32) (r int32) {
	panic("not implemeneted")
}

var __func__4 = [10]int8{'v', 'f', 's', 'D', 'e', 'l', 'e', 't', 'e'}

// C documentation
//
//	/*
//	** The following four VFS methods:
//	**
//	**   xDlOpen
//	**   xDlError
//	**   xDlSym
//	**   xDlClose
//	**
//	** are supposed to implement the functionality needed by SQLite to load
//	** extensions compiled as shared objects. This simple VFS does not support
//	** this functionality, so the following functions are no-ops.
//	*/
func vfsDlOpen(tls *libc.TLS, pVfs uintptr, zPath uintptr) (r uintptr) {
	return uintptr(0)
}

func vfsDlError(tls *libc.TLS, pVfs uintptr, nByte int32, zErrMsg uintptr) {
	libsqlite3.Xsqlite3_snprintf(tls, nByte, zErrMsg, __ccgo_ts+68, 0)
	*(*int8)(unsafe.Pointer(zErrMsg + uintptr(nByte-int32(1)))) = int8('\000')
}

func vfsDlSym(tls *libc.TLS, pVfs uintptr, pH uintptr, z uintptr) (r uintptr) {
	return uintptr(0)
}

func vfsDlClose(tls *libc.TLS, pVfs uintptr, pHandle uintptr) {
	return
}

// C documentation
//
//	/*
//	** Parameter zByte points to a buffer nByte bytes in size. Populate this
//	** buffer with pseudo-random data.
//	*/
func vfsRandomness(tls *libc.TLS, pVfs uintptr, nByte int32, zByte uintptr) (r int32) {
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** Sleep for at least nMicro microseconds. Return the (approximate) number
//	** of microseconds slept for.
//	*/
func vfsSleep(tls *libc.TLS, pVfs uintptr, nMicro int32) (r int32) {
	time.Sleep(time.Duration(nMicro) * time.Microsecond)
	return nMicro
}

// C documentation
//
//	/*
//	** Set *pTime to the current UTC time expressed as a Julian day. Return
//	** SQLITE_OK if successful, or an error code otherwise.
//	**
//	**   http://en.wikipedia.org/wiki/Julian_day
//	**
//	** This implementation is not very good. The current time is rounded to
//	** an integer number of seconds. Also, assuming time_t is a signed 32-bit
//	** value, it will stop working some time in the year 2038 AD (the so-called
//	** "year 2038" problem that afflicts systems that store time this way).
//	*/
func vfsCurrentTime(tls *libc.TLS, pVfs uintptr, pTime uintptr) (r int32) {
	t := time.Now().Unix()
	*(*float64)(unsafe.Pointer(pTime)) = float64(float64(t))/float64(86400) + float64(2.4405875e+06)
	return SQLITE_OK
}

// C documentation
//
//	/*
//	** This function returns a pointer to the VFS implemented in this file.
//	** To make the VFS available to SQLite:
//	**
//	**   sqlite3_vfs_register(sqlite3_fsFS(), 0);
//	*/
func Xsqlite3_fsFS(tls *libc.TLS, zName uintptr, pAppData uintptr) (r uintptr) {
	var p uintptr = libc.Xmalloc(tls, types.Size_t(unsafe.Sizeof(sqlite3_vfs{})))
	if !(p != 0) {
		return libc.UintptrFromInt32(0)
	}
	*(*sqlite3_vfs)(unsafe.Pointer(p)) = sqlite3_vfs{
		iVersion:      int32(1),
		szOsFile:      int32(32),
		mxPathname:    int32(MAXPATHNAME),
		zName:         zName,
		pAppData:      pAppData,
		xOpen:         __ccgo_fp(vfsOpen),
		xDelete:       __ccgo_fp(vfsDelete),
		xAccess:       __ccgo_fp(vfsAccess),
		xFullPathname: __ccgo_fp(vfsFullPathname),
		xDlOpen:       __ccgo_fp(vfsDlOpen),
		xDlError:      __ccgo_fp(vfsDlError),
		xDlSym:        __ccgo_fp(vfsDlSym),
		xDlClose:      __ccgo_fp(vfsDlClose),
		xRandomness:   __ccgo_fp(vfsRandomness),
		xSleep:        __ccgo_fp(vfsSleep),
		xCurrentTime:  __ccgo_fp(vfsCurrentTime),
	}
	return p
}

func __ccgo_fp(f interface{}) uintptr {
	type iface [2]uintptr
	return (*iface)(unsafe.Pointer(&f))[1]
}

var __ccgo_ts = (*reflect.StringHeader)(unsafe.Pointer(&__ccgo_ts1)).Data

var __ccgo_ts1 = "TODO %s:%i:\n\x00p->nBuffer==0 || p->iBufferOfst+p->nBuffer==i\x00vfs.c\x00%s\x00Loadable extensions are not supported\x00"
