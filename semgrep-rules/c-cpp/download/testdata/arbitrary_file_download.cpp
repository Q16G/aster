// C++ 任意文件读取测试用例（仅供 semgrep 规则验证）
// 注意：std::ifstream fs(argv[1]) 这种构造声明形式 semgrep 解析有限制，
// 推荐写成 fs.open(...) 的拆分形式以便静态分析覆盖
#include <fstream>
#include <filesystem>
#include <string>
#include <cstdlib>

void cppOpenArgv(int argc, char **argv) {
    std::ifstream fs;
    // ruleid: c-download-arbitrary-file
    fs.open(argv[1]);
}

void cppOpenEnv() {
    const char *p = std::getenv("FILE_PATH");
    std::ofstream fs;
    // ruleid: c-download-arbitrary-file
    fs.open(p);
}

void cppCopyArbitrary(const std::string &src) {
    // ruleid: c-download-arbitrary-file
    std::filesystem::copy(src, "/tmp/dst");
}

void cppDirIter(const std::string &dir) {
    // ruleid: c-download-arbitrary-file
    for (auto &e : std::filesystem::directory_iterator(dir)) {
        (void)e;
    }
}

void cppCanonical(const std::string &p) {
    // ruleid: c-download-arbitrary-file
    auto c = std::filesystem::canonical(p);
}

void cppRemove(const std::string &p) {
    // ruleid: c-download-arbitrary-file
    std::filesystem::remove(p);
}

// ============= 安全写法 =============

void safeFixedCpp() {
    std::ifstream fs;
    // ok: c-download-arbitrary-file
    fs.open("/var/data/report.csv");
}

void safeBasenameCpp(const std::string &user) {
    std::ifstream fs;
    // ok: c-download-arbitrary-file
    fs.open(basename((char *)user.c_str()));
}

void safeFsCopyLiteral() {
    // ok: c-download-arbitrary-file
    std::filesystem::copy("/var/data/a", "/var/data/b");
}
