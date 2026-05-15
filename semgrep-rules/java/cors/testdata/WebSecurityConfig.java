import io.jsonwebtoken.security.Keys;
import java.io.File;
import java.nio.file.Paths;
import org.springframework.web.bind.annotation.CrossOrigin;
import org.springframework.web.cors.CorsConfiguration;
import org.springframework.web.multipart.MultipartFile;
import org.springframework.security.config.annotation.web.builders.HttpSecurity;

class WebSecurityConfig {
    @CrossOrigin(origins = "*", allowCredentials = "true")
    void wildcard() {}

    void insecureCorsConfig() {
        CorsConfiguration cfg = new CorsConfiguration();
        // ruleid: java-cors-wildcard-credentials
        cfg.addAllowedOrigin("*");
        cfg.setAllowCredentials(true);
    }

    void secureCorsConfig() {
        CorsConfiguration cfg = new CorsConfiguration();
        // ok: java-cors-wildcard-credentials
        cfg.addAllowedOrigin("https://example.com");
        cfg.setAllowCredentials(true);
    }

    void disableCsrf(HttpSecurity http) throws Exception {
        // ruleid: java-spring-csrf-disabled
        http.csrf().disable();
    }

    void hardcodedJwt() {
        // ruleid: java-hardcoded-jwt-secret
        Keys.hmacShaKeyFor("super-secret-super-secret".getBytes());
    }

    void safeJwt(byte[] secret) {
        // ok: java-hardcoded-jwt-secret
        Keys.hmacShaKeyFor(secret);
    }

    void upload(MultipartFile file, String dir) throws Exception {
        // ruleid: java-upload-original-filename-path
        file.transferTo(new File(dir, file.getOriginalFilename()));
        File dest = new File(dir, file.getOriginalFilename());
        // ruleid: java-upload-original-filename-path
        file.transferTo(dest);
    }

    void safeUpload(MultipartFile file, String dir) throws Exception {
        // ok: java-upload-original-filename-path
        file.transferTo(new File(dir, org.apache.commons.io.FilenameUtils.getName(file.getOriginalFilename())));
    }
}
