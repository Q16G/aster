package com.example.testdata;

import javax.servlet.http.HttpServletRequest;
import javax.servlet.http.HttpServletResponse;

public class CorsOriginReflection {

    // ruleid: java-cors-origin-reflection
    public void reflectOriginInline(HttpServletRequest request, HttpServletResponse response) {
        response.setHeader("Access-Control-Allow-Origin", request.getHeader("Origin"));
    }

    // ruleid: java-cors-origin-reflection
    public void reflectOriginVariable(HttpServletRequest request, HttpServletResponse response) {
        String origin = request.getHeader("Origin");
        response.setHeader("Access-Control-Allow-Origin", origin);
    }

    // ok: java-cors-origin-reflection
    public void staticOrigin(HttpServletRequest request, HttpServletResponse response) {
        response.setHeader("Access-Control-Allow-Origin", "https://trusted.example.com");
    }

    // ok: java-cors-origin-reflection
    public void wildcardOrigin(HttpServletRequest request, HttpServletResponse response) {
        response.setHeader("Access-Control-Allow-Origin", "*");
    }
}
