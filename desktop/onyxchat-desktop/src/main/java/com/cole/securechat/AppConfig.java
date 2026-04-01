package com.cole.securechat.util;

public class AppConfig {

    // onyx.env = dev | prod
    private static final String ENV =
            System.getProperty("onyx.env",
                            System.getenv().getOrDefault("ONYX_ENV", "dev"))
                    .trim().toLowerCase();


    public static final String SERVER_BASE_URL;
    public static final String API_BASE;

    // Runtime auth token (set after login)
    public static volatile String AUTH_TOKEN = null;

    static {
        switch (ENV) {
            case "prod" -> SERVER_BASE_URL = "https://api.onyxchat.dev";
            default -> SERVER_BASE_URL = "http://127.0.0.1:8080";
        }

        API_BASE = SERVER_BASE_URL + "/api/v1";
    }
    public static boolean isProd() {
        return "prod".equals(ENV);
    }


}
