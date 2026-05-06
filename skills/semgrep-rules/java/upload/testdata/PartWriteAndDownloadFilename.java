import java.io.IOException;
import javax.servlet.http.HttpServletResponse;
import javax.servlet.http.Part;

class PartWriteAndDownloadFilename {
    void insecureUpload(Part part, String dir) throws IOException {
        // ruleid: java-upload-part-write-and-download-filename
        part.write(dir + part.getSubmittedFileName());
        String dest = java.nio.file.Paths.get(dir, part.getSubmittedFileName()).toString();
        // ruleid: java-upload-part-write-and-download-filename
        part.write(dest);
    }

    void insecureDownload(HttpServletResponse response, String filename) {
        // ruleid: java-upload-part-write-and-download-filename
        response.setHeader("Content-Disposition", "attachment; filename=" + filename);
    }

    void safeUpload(Part part, String dir) throws IOException {
        // ok: java-upload-part-write-and-download-filename
        part.write(java.nio.file.Paths.get(dir, java.nio.file.Paths.get(part.getSubmittedFileName()).getFileName().toString()).toString());
    }
}
