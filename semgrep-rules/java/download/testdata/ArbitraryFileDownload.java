// 综合测试：文件下载 / 任意文件读取漏洞各种组件场景
// 注意：这个文件只用于 semgrep 规则测试，不要求能实际编译通过
import java.io.File;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.RandomAccessFile;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.zip.ZipFile;
import javax.servlet.ServletContext;
import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;
import javax.ws.rs.GET;
import javax.ws.rs.PathParam;
import javax.ws.rs.QueryParam;
import org.apache.commons.io.FileUtils;
import org.apache.commons.io.FilenameUtils;
import org.springframework.core.io.FileSystemResource;
import org.springframework.core.io.UrlResource;
import org.springframework.web.bind.annotation.CookieValue;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RequestHeader;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.multipart.MultipartFile;
import cn.hutool.core.io.FileUtil;

class ArbitraryFileDownload {

    // ============================================================
    // Rule A: java-download-arbitrary-file (HIGH, 完整路径直接型)
    // ============================================================

    void servletStream(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        // ruleid: java-download-arbitrary-file
        FileInputStream fis = new FileInputStream(req.getParameter("path"));
        fis.transferTo(resp.getOutputStream());
        fis.close();
    }

    void servletHeader(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        // ruleid: java-download-arbitrary-file
        File f = new File(req.getHeader("X-File"));
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    void servletRandomAccess(HttpServletRequest req) throws IOException {
        // ruleid: java-download-arbitrary-file
        new RandomAccessFile(req.getParameter("p"), "r").close();
    }

    @GetMapping("/asResource")
    public FileSystemResource springResource(@RequestParam String filename) {
        // ruleid: java-download-arbitrary-file
        return new FileSystemResource(filename);
    }

    @GetMapping("/asUrlResource")
    public UrlResource springUrlResource(@RequestParam String url) throws IOException {
        // ruleid: java-download-arbitrary-file
        return new UrlResource(url);
    }

    @GET
    public InputStream jaxrsPath(@PathParam("name") String name) throws IOException {
        // ruleid: java-download-arbitrary-file
        return Files.newInputStream(Paths.get(name));
    }

    void hutoolDirect(HttpServletRequest req) throws IOException {
        // ruleid: java-download-arbitrary-file
        FileUtil.readUtf8String(req.getParameter("p"));
    }

    void servletCtxRealPath(ServletContext ctx, HttpServletRequest req) {
        // ruleid: java-download-arbitrary-file
        ctx.getRealPath(req.getParameter("p"));
    }

    void classLoader(HttpServletRequest req) {
        ClassLoader cl = getClass().getClassLoader();
        // ruleid: java-download-arbitrary-file
        cl.getResourceAsStream(req.getParameter("p"));
    }

    void zipExtract(HttpServletRequest req) throws IOException {
        // ruleid: java-download-arbitrary-file
        ZipFile zf = new ZipFile(req.getParameter("p"));
        zf.close();
    }

    @GetMapping("/byCookie")
    public byte[] springCookie(@CookieValue("token") String t) throws IOException {
        // ruleid: java-download-arbitrary-file
        return Files.readAllBytes(Paths.get(t));
    }

    // 通过 StringBuilder 间接拼接（验证 propagator）
    void propagationViaSB(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        StringBuilder sb = new StringBuilder("/var/data/");
        sb.append(req.getParameter("name"));
        // ruleid: java-download-arbitrary-file
        Files.copy(Paths.get(sb.toString()), resp.getOutputStream());
    }

    // ============================================================
    // Rule B: java-download-arbitrary-file-joined (MEDIUM, 拼接型)
    // ============================================================

    void servletJoinedTwoArg(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        String filename = req.getParameter("file");
        // ruleid: java-download-arbitrary-file-joined
        File f = new File("/var/data/", filename);
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    void servletReadAllJoined(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        // ruleid: java-download-arbitrary-file-joined
        byte[] data = Files.readAllBytes(Paths.get("/var/data/", req.getParameter("name")));
        resp.getOutputStream().write(data);
    }

    @GetMapping("/file/{name}")
    public void springPathJoined(@PathVariable String name, HttpServletResponse resp) throws IOException {
        // ruleid: java-download-arbitrary-file-joined
        FileInputStream fis = new FileInputStream(new File("/var/data/" + name));
        fis.transferTo(resp.getOutputStream());
        fis.close();
    }

    @GetMapping("/byHeader")
    public byte[] springHeaderJoined(@RequestHeader("X-File") String f) throws IOException {
        // ruleid: java-download-arbitrary-file-joined
        return FileUtils.readFileToByteArray(new File("/var/data/", f));
    }

    @GET
    public byte[] jaxrsQueryJoined(@QueryParam("p") String p) throws IOException {
        // ruleid: java-download-arbitrary-file-joined
        return Files.readAllBytes(Paths.get("/var/data/", p));
    }

    void uploadOriginalFilename(MultipartFile file) throws IOException {
        // ruleid: java-download-arbitrary-file-joined
        File dst = new File("/var/upload/", file.getOriginalFilename());
        file.transferTo(dst);
    }

    // ============================================================
    // 安全写法（不应被任一规则命中）
    // ============================================================

    // 1. FilenameUtils.getName 净化
    void safeWithFilenameUtils(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        // ok: java-download-arbitrary-file-joined
        File f = new File("/var/data/", FilenameUtils.getName(req.getParameter("file")));
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    // 2. Path.getFileName 净化
    void safeWithGetFileName(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        String raw = req.getParameter("file");
        String safe = Paths.get(raw).getFileName().toString();
        // ok: java-download-arbitrary-file-joined
        File f = new File("/var/data/", safe);
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    // 3. 用 Long.parseLong 把用户输入约束为整数后再用作文件名段
    void safeWithIdLookup(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        // ok: java-download-arbitrary-file-joined
        File f = new File("/var/data/", String.valueOf(Long.parseLong(req.getParameter("id"))) + ".bin");
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    // 4. 固定文件名
    void safeFixed(HttpServletResponse resp) throws IOException {
        // ok: java-download-arbitrary-file-joined
        File f = new File("/var/data/", "report.csv");
        Files.copy(f.toPath(), resp.getOutputStream());
    }

    // 5. 业务拼接也是漏洞：name 可以是绝对路径或 ../，所以仍然命中 Rule A
    void unsafeBusinessConcat(HttpServletRequest req) {
        String name = req.getParameter("name");
        // ruleid: java-download-arbitrary-file
        File f = new File(name + ".log");
        f.getName();
    }

    // 6. 自定义校验函数（按命名约定识别为 sanitizer）
    void safeCustomSanitizer(HttpServletRequest req, HttpServletResponse resp) throws IOException {
        String validated = PathUtils.validatePath(req.getParameter("file"));
        // ok: java-download-arbitrary-file-joined
        File f = new File("/var/data/", validated);
        Files.copy(f.toPath(), resp.getOutputStream());
    }
}

class PathUtils {
    static String validatePath(String p) {
        if (p == null || p.contains("..")) throw new IllegalArgumentException();
        return p;
    }
}
