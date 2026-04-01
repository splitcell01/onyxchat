package com.cole.securechat.api;

import com.cole.securechat.util.AppConfig;
import org.json.JSONArray;
import org.json.JSONObject;

import java.io.IOException;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;

public class ApiClient {

    private final HttpClient httpClient = HttpClient.newHttpClient();
    private final String baseUrl;

    private long currentUserId;
    private String currentUsername;
    private String authToken;

    public ApiClient() {
        this.baseUrl = AppConfig.SERVER_BASE_URL;
    }

    public void setCurrentUser(long id, String username, String token) {
        this.currentUserId = id;
        this.currentUsername = username;
        this.authToken = token;
    }

    // ─────────────────────────────────────
    // Send a message
    // ─────────────────────────────────────
    public void sendMessage(String recipientUsername, String body) throws IOException, InterruptedException {
        String clientMessageId = UUID.randomUUID().toString();

        JSONObject json = new JSONObject();
        json.put("recipientUsername", recipientUsername);
        json.put("body", body);
        json.put("clientMessageId", clientMessageId);

        String url = baseUrl + "/api/v1/messages";

        HttpRequest request = HttpRequest.newBuilder()
                .uri(URI.create(url))
                .header("Content-Type", "application/json")
                .header("Authorization", "Bearer " + authToken)
                .POST(HttpRequest.BodyPublishers.ofString(json.toString()))
                .build();

        HttpResponse<String> response =
                httpClient.send(request, HttpResponse.BodyHandlers.ofString());

        if (response.statusCode() != 200 && response.statusCode() != 201) {
            throw new IOException("sendMessage failed: " + response.statusCode() + " " + response.body());
        }
    }

    // ─────────────────────────────────────
    // Fetch new messages since last ID
    // ─────────────────────────────────────
    public static class MessageDto {
        public long id;
        public long senderId;
        public long recipientId;
        public String body;
        public String createdAt;
    }

    public List<MessageDto> fetchMessages(String peerUsername, long sinceId)
            throws IOException, InterruptedException {

        String url = String.format("%s/api/v1/messages?peer=%s&sinceId=%d",
                baseUrl,
                URLEncoder.encode(peerUsername, StandardCharsets.UTF_8),
                sinceId);

        HttpRequest request = HttpRequest.newBuilder()
                .uri(URI.create(url))
                .header("Authorization", "Bearer " + authToken)
                .GET()
                .build();

        HttpResponse<String> response =
                httpClient.send(request, HttpResponse.BodyHandlers.ofString());

        if (response.statusCode() != 200) {
            throw new IOException("fetchMessages failed: " + response.statusCode() + " " + response.body());
        }

        return parseMessages(response.body());
    }

    private List<MessageDto> parseMessages(String json) {
        List<MessageDto> result = new ArrayList<>();

        JSONObject root = new JSONObject(json);
        JSONArray arr = root.getJSONArray("messages");

        for (int i = 0; i < arr.length(); i++) {
            JSONObject m = arr.getJSONObject(i);
            MessageDto dto = new MessageDto();
            dto.id = m.getLong("id");
            dto.senderId = m.getLong("senderId");
            dto.recipientId = m.getLong("recipientId");
            dto.body = m.getString("body");
            dto.createdAt = m.getString("createdAt");
            result.add(dto);
        }

        return result;
    }
}