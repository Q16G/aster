// Java JWT algorithm-confusion / claim 校验缺失 测试用例
// 仅用于 semgrep 规则验证
package testdata;

import java.security.interfaces.RSAPublicKey;
import java.security.Key;

import com.auth0.jwt.JWT;
import com.auth0.jwt.algorithms.Algorithm;
import com.auth0.jwt.interfaces.DecodedJWT;
import com.auth0.jwt.interfaces.JWTVerifier;

import io.jsonwebtoken.Jwts;

public class JwtAlgorithmConfusion {

    private RSAPublicKey publicKey;
    private Key sharedKey;

    // ============= auth0 java-jwt: 算法来自 token header（HIGH） =============

    public DecodedJWT verifyFromHeaderAlg(String token) {
        DecodedJWT jwt = JWT.decode(token);
        // ruleid: java-jwt-algorithm-confusion-from-header
        Algorithm algorithm = Algorithm.HMAC256(jwt.getAlgorithm());
        JWTVerifier verifier = JWT.require(algorithm).build();
        return verifier.verify(token);
    }

    public DecodedJWT verifyHmacWithPublicKey(String token) {
        // ruleid: java-jwt-algorithm-confusion-from-header
        Algorithm algorithm = Algorithm.HMAC256(publicKey.getEncoded());
        return JWT.require(algorithm).build().verify(token);
    }

    // ============= jjwt: setSigningKey 没绑定算法（MEDIUM） =============

    public Object jjwtSetSigningKey(String token) {
        // ruleid: java-jwt-jjwt-algorithm-confusion
        return Jwts.parserBuilder().setSigningKey(publicKey).build().parseClaimsJws(token);
    }

    public Object jjwtSetSigningKeyOld(String token) {
        // ruleid: java-jwt-jjwt-algorithm-confusion
        return Jwts.parser().setSigningKey(publicKey).parseClaimsJws(token);
    }

    // ============= 安全写法（不应被命中） =============

    public DecodedJWT verifySafeRSA(String token) {
        // ok: java-jwt-algorithm-confusion-from-header
        Algorithm algorithm = Algorithm.RSA256(publicKey, null);
        return JWT.require(algorithm).withIssuer("https://issuer/").build().verify(token);
    }

    // ============= missing claim validation =============

    public String getUserFromToken(String token) {
        DecodedJWT jwt = JWT.decode(token);
        // ruleid: java-jwt-missing-claim-validation
        return jwt.getClaim("sub").asString();
    }

    public String getUserSafe(String token) {
        Algorithm algorithm = Algorithm.RSA256(publicKey, null);
        DecodedJWT jwt = JWT.decode(token);
        // ok: java-jwt-missing-claim-validation
        JWT.require(algorithm).withIssuer("https://issuer/").build().verify(token);
        return jwt.getClaim("sub").asString();
    }
}
