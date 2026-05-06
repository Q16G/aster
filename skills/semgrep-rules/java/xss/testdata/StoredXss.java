import java.io.IOException;
import javax.servlet.http.HttpServletResponse;
import org.springframework.http.ResponseEntity;

class StoredXssController {
    String renderPost(Post post) {
        // ruleid: java-xss-stored-content-response
        return post.getContent();
    }

    ResponseEntity<String> renderComment(Comment comment) {
        // ruleid: java-xss-stored-content-response
        return ResponseEntity.ok(comment.getBody());
    }

    void renderProfile(Profile profile, HttpServletResponse response) throws IOException {
        // ruleid: java-xss-stored-content-response
        response.getWriter().write(profile.getRenderedHtml());
    }

    String renderStatic() {
        // ok: java-xss-stored-content-response
        return "<p>trusted</p>";
    }
}

class Post {
    String getContent() { return ""; }
}

class Comment {
    String getBody() { return ""; }
}

class Profile {
    String getRenderedHtml() { return ""; }
}
