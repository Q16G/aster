/* C 任意文件读取测试用例（仅供 semgrep 规则验证） */
#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>
#include <libgen.h>
#include <dirent.h>

void from_argv(int argc, char **argv) {
    /* ruleid: c-download-arbitrary-file */
    FILE *fp = fopen(argv[1], "r");
    fclose(fp);
}

void from_env(void) {
    char *p = getenv("FILE_PATH");
    /* ruleid: c-download-arbitrary-file */
    int fd = open(p, O_RDONLY);
    close(fd);
}

void cgi_query(const char *qs) {
    /* ruleid: c-download-arbitrary-file */
    FILE *fp = fopen(qs, "rb");
    fclose(fp);
}

void stat_check(const char *p) {
    struct stat st;
    /* ruleid: c-download-arbitrary-file */
    stat(p, &st);
}

void open_dir(const char *p) {
    /* ruleid: c-download-arbitrary-file */
    DIR *d = opendir(p);
    closedir(d);
}

void format_then_open(const char *p) {
    char buf[256];
    /* ruleid: c-download-arbitrary-file */
    snprintf(buf, sizeof(buf), "/var/data/%s", p);
    fopen(buf, "r");
}

void cat_via_system(const char *p) {
    char buf[256];
    /* ruleid: c-download-arbitrary-file */
    snprintf(buf, sizeof(buf), "cat %s", p);
    system(buf);
}

/* ============= 安全写法 ============= */

void safe_fixed(void) {
    /* ok: c-download-arbitrary-file */
    FILE *fp = fopen("/var/data/report.csv", "r");
    fclose(fp);
}

void safe_basename(const char *user) {
    /* ok: c-download-arbitrary-file */
    FILE *fp = fopen(basename((char *)user), "r");
    fclose(fp);
}
