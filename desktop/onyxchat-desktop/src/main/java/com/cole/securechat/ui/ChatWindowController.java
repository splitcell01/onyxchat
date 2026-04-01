package com.cole.securechat.ui;

import com.cole.securechat.util.AppConfig;
import javafx.application.Platform;
import javafx.fxml.FXML;
import javafx.fxml.FXMLLoader;
import javafx.scene.control.Button;
import javafx.scene.control.ListView;
import javafx.scene.control.TextField;
import javafx.scene.control.ListCell;
import javafx.scene.Parent;
import javafx.scene.Scene;
import javafx.stage.Stage;
import javafx.scene.control.Label;
import javafx.scene.layout.HBox;
import javafx.scene.paint.Color;
import javafx.scene.shape.Circle;
import javafx.geometry.Pos;
import javafx.geometry.Insets;
import javafx.scene.layout.VBox;
import javafx.scene.layout.Region;
import javafx.scene.control.ScrollPane;


import org.json.JSONArray;
import org.json.JSONObject;

import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.net.http.WebSocket;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.concurrent.*;
import java.util.concurrent.ConcurrentHashMap;
import java.util.UUID;

public class ChatWindowController {

    @FXML
    private ListView<String> contactsList;   // left pane contacts

    @FXML
    private TextField messageField;

    @FXML
    private Button sendButton;

    @FXML
    private Button logoutButton;

    @FXML
    private Label typingLabel;

    // New header fields (from modern FXML)
    @FXML
    private Label activeConversationLabel;

    @FXML
    private Circle activeStatus;

    // New message area (VBox inside ScrollPane)
    @FXML
    private ScrollPane messageScroll;

    @FXML
    private VBox messageContainer;

    private final HttpClient httpClient = HttpClient.newHttpClient();

    private long currentUserId;
    private String currentUsername;
    private String authToken;

    private volatile long lastMessageId = 0;
    private WebSocket webSocket;

    private final Map<String, Boolean> onlineStatus = new ConcurrentHashMap<>();

    // Typing indicator state
    private final ScheduledExecutorService typingExecutor =
            Executors.newSingleThreadScheduledExecutor();
    private volatile boolean currentlyTyping = false;
    private ScheduledFuture<?> typingStopFuture;

    // Called by LoginController after successful login
    public void setCurrentUser(long userId, String username, String authToken) {
        this.currentUserId = userId;
        this.currentUsername = username;
        this.authToken = authToken;

        // WebSocket for new messages
        connectWebSocket();
        // Load contacts from server
        loadContactsFromServer();
    }

    private void loadMessages(String peer) {
        // OLD:
        // messagesList.getItems().clear();
        // NEW:
        messageContainer.getChildren().clear();
        lastMessageId = 0;

        if (peer != null && authToken != null && !authToken.isEmpty()) {
            new Thread(() -> fetchMessagesFromServer(peer), "messages-loader").start();
        }
    }



    @FXML
    private void initialize() {
        // Contacts rendering with colored presence dot
        contactsList.setCellFactory(lv -> new ListCell<>() {
            private final HBox root = new HBox(8);
            private final Circle dot = new Circle(4);
            private final Label nameLabel = new Label();

            {
                root.getChildren().addAll(dot, nameLabel);
                root.getStyleClass().add("contact-cell");
                dot.getStyleClass().add("presence-indicator");
            }

            @Override
            protected void updateItem(String item, boolean empty) {
                super.updateItem(item, empty);
                if (empty || item == null) {
                    setGraphic(null);
                    setText(null);
                } else {
                    boolean isOnline = onlineStatus.getOrDefault(item, false);

                    // Reset state classes
                    dot.getStyleClass().removeAll("online", "offline");
                    dot.getStyleClass().add(isOnline ? "online" : "offline");

                    nameLabel.setText(item);
                    setText(null);
                    setGraphic(root);
                }
            }
        });

        // When you click a contact, load their history
        contactsList.getSelectionModel().selectedItemProperty()
                .addListener((obs, oldVal, newVal) -> {
                    if (newVal != null) {
                        updateChatHeader(newVal);
                        loadMessages(newVal);
                    }
                });

        sendButton.setOnAction(e -> sendMessage());

        // Typing label initial state
        if (typingLabel != null) {
            typingLabel.setText("");
            typingLabel.setVisible(false);
        }

        setupTypingIndicator();
    }

    // ─────────────────────────────────────────────────────────────
    // Typing indicator (client-side)
    // ─────────────────────────────────────────────────────────────

    private void setupTypingIndicator() {
        if (messageField == null) return;

        messageField.textProperty().addListener((obs, oldVal, newVal) -> {
            handleTypingChanged(newVal);
        });
    }

    private void handleTypingChanged(String text) {
        if (webSocket == null || authToken == null || authToken.isEmpty()) return;
        String peer = getSelectedContact();
        if (peer == null) return;

        // If user wasn't typing, notify start
        if (!currentlyTyping) {
            currentlyTyping = true;
            sendTypingEvent(true, peer);
        }

        // Reset the "stop typing" timer
        if (typingStopFuture != null) {
            typingStopFuture.cancel(false);
        }
        typingStopFuture = typingExecutor.schedule(() -> {
            currentlyTyping = false;
            sendTypingEvent(false, peer);
        }, 2, TimeUnit.SECONDS);   // 2s of inactivity = stopped
    }

    private void sendTypingEvent(boolean isTyping, String peer) {
        try {
            if (webSocket == null) return;
            JSONObject obj = new JSONObject();
            obj.put("type", "typing");
            obj.put("to", peer);
            obj.put("isTyping", isTyping);
            webSocket.sendText(obj.toString(), true);
        } catch (Exception e) {
            e.printStackTrace();
        }
    }

    private String getSelectedContact() {
        return contactsList.getSelectionModel().getSelectedItem();
    }

    // ─────────────────────────────────────────────────────────────
    // Contacts (GET /api/v1/users)
    // ─────────────────────────────────────────────────────────────

    private void loadContactsFromServer() {
        if (authToken == null || authToken.isEmpty()) {
            System.err.println("No auth token; cannot load contacts.");
            return;
        }

        new Thread(() -> {
            try {
                String url = AppConfig.API_BASE + "/users";

                HttpRequest req = HttpRequest.newBuilder()
                        .uri(URI.create(url))
                        .header("Authorization", "Bearer " + authToken)
                        .GET()
                        .build();

                HttpResponse<String> resp = httpClient.send(req, HttpResponse.BodyHandlers.ofString());
                if (resp.statusCode() != 200) {
                    System.err.println("loadContacts failed: " + resp.statusCode() + " " + resp.body());
                    return;
                }

                JSONArray arr = new JSONArray(resp.body());
                var items = new java.util.ArrayList<String>();
                for (int i = 0; i < arr.length(); i++) {
                    JSONObject obj = arr.getJSONObject(i);
                    String uname = obj.getString("username");
                    if (!uname.equals(currentUsername)) {
                        items.add(uname);
                        onlineStatus.putIfAbsent(uname, false); // default offline
                    }
                }

                Platform.runLater(() -> {
                    contactsList.getItems().setAll(items);
                    if (!contactsList.getItems().isEmpty()) {
                        contactsList.getSelectionModel().selectFirst();
                    }
                });

            } catch (Exception e) {
                e.printStackTrace();
            }
        }, "contacts-loader").start();
    }

    // ─────────────────────────────────────────────────────────────
    // Sending messages (POST /api/v1/messages)
    // ─────────────────────────────────────────────────────────────

    @FXML
    private void sendMessage() {
        String peer = getSelectedContact();
        String body = messageField.getText().trim();

        if (peer == null || peer.isEmpty()) {
            System.err.println("No contact selected.");
            return;
        }
        if (body.isEmpty()) {
            return;
        }
        if (authToken == null || authToken.isEmpty()) {
            System.err.println("No auth token; cannot send message.");
            return;
        }

        new Thread(() -> {
            try {
                String url = AppConfig.API_BASE + "/messages";

                JSONObject json = new JSONObject();
                json.put("recipientUsername", peer);
                json.put("body", body);
                json.put("clientMessageId", java.util.UUID.randomUUID().toString());

                HttpRequest req = HttpRequest.newBuilder()
                        .uri(URI.create(url))
                        .header("Content-Type", "application/json")
                        .header("Authorization", "Bearer " + authToken)
                        .POST(HttpRequest.BodyPublishers.ofString(json.toString()))
                        .build();

                HttpResponse<String> resp = httpClient.send(req, HttpResponse.BodyHandlers.ofString());
                if (resp.statusCode() == 200 || resp.statusCode() == 201) {
                    Platform.runLater(() -> messageField.clear());
                } else {
                    System.err.println("send failed: " + resp.statusCode() + " " + resp.body());
                }

            } catch (Exception e) {
                e.printStackTrace();
            }
        }, "send-thread").start();
    }

    // ─────────────────────────────────────────────────────────────
    // Polling for new messages (GET /api/v1/messages)
    // ─────────────────────────────────────────────────────────────

    private void fetchMessagesFromServer(String peer) {
        if (authToken == null || authToken.isEmpty()) {
            System.err.println("No auth token; cannot fetch messages.");
            return;
        }

        try {
            String queryPeer = URLEncoder.encode(peer, StandardCharsets.UTF_8);
            String url = AppConfig.API_BASE + "/messages?peer=" + queryPeer;

            if (lastMessageId > 0) {
                url += "&sinceId=" + lastMessageId;
            }

            HttpRequest request = HttpRequest.newBuilder()
                    .uri(URI.create(url))
                    .header("Authorization", "Bearer " + authToken)
                    .GET()
                    .build();

            HttpResponse<String> response =
                    httpClient.send(request, HttpResponse.BodyHandlers.ofString());

            if (response.statusCode() == 200) {
                String body = response.body();
                JSONObject obj = new JSONObject(body);
                JSONArray arr = obj.getJSONArray("messages");

                if (arr.length() == 0) {
                    return;
                }

                java.util.List<MessageDto> list = new java.util.ArrayList<>();
                for (int i = 0; i < arr.length(); i++) {
                    JSONObject m = arr.getJSONObject(i);
                    MessageDto dto = new MessageDto();
                    dto.id = m.getLong("id");
                    dto.senderId = m.getLong("senderId");
                    dto.recipientId = m.getLong("recipientId");
                    dto.body = m.getString("body");
                    dto.createdAt = m.getString("createdAt");
                    list.add(dto);

                    if (dto.id > lastMessageId) {
                        lastMessageId = dto.id;
                    }
                }

                Platform.runLater(() -> {
                    for (MessageDto dto : list) {
                        addMessageToList(peer, dto);
                    }
                });
            } else {
                System.err.println("fetchMessages failed: " + response.statusCode() + " " + response.body());
            }

        } catch (Exception e) {
            e.printStackTrace();
        }
    }

    private void updateChatHeader(String peer) {
        if (activeConversationLabel != null) {
            activeConversationLabel.setText(peer);
        }
        if (activeStatus != null) {
            boolean online = onlineStatus.getOrDefault(peer, false);
            activeStatus.setFill(online ? Color.LIMEGREEN : Color.DARKGRAY);
        }
    }


    // mirror of Go Message struct fields
    public static class MessageDto {
        public long id;
        public long senderId;
        public long recipientId;
        public String body;
        public String createdAt;
    }

    private void addMessageToList(String peerName, MessageDto dto) {
        boolean isMe = dto.senderId == currentUserId;

        // Main bubble text – just the body; timestamp is separate
        Label bubble = new Label(dto.body);
        bubble.getStyleClass().addAll("message-bubble", isMe ? "me" : "them");
        bubble.setWrapText(true);
        bubble.setMaxWidth(360); // matches CSS max-width

        HBox row = new HBox(bubble);
        row.getStyleClass().addAll("message-row", isMe ? "me" : "them");

        // Optional timestamp under the bubble
        Label tsLabel = new Label(formatTimestamp(dto.createdAt));
        tsLabel.getStyleClass().add("message-timestamp");

        VBox wrapper = new VBox(row, tsLabel);
        wrapper.setSpacing(2);
        wrapper.setFillWidth(false);
        wrapper.setPadding(new Insets(2, 0, 2, 0));

        messageContainer.getChildren().add(wrapper);
        scrollMessagesToBottom();
    }

    private void scrollMessagesToBottom() {
        if (messageScroll == null) return;

        Platform.runLater(() -> {
            // Force layout so ScrollPane knows new height
            messageScroll.layout();
            messageScroll.setVvalue(1.0);
        });
    }


    private String formatTimestamp(String createdAt) {
        // TODO: parse/pretty-print if you want; for now just show the raw string
        return createdAt;
    }

    // ─────────────────────────────────────────────────────────────
    // Logout
    // ─────────────────────────────────────────────────────────────

    @FXML
    private void handleLogout() {
        try {
            // Close WebSocket if open
            if (webSocket != null) {
                try {
                    webSocket.sendClose(WebSocket.NORMAL_CLOSURE, "logout").join();
                } catch (Exception ignored) {
                }
                webSocket = null;
            }

            // Stop typing timer / executor
            if (typingStopFuture != null) {
                typingStopFuture.cancel(false);
            }
            typingExecutor.shutdownNow();

            // Clear user/session state
            this.currentUserId = 0;
            this.currentUsername = null;
            this.authToken = null;
            this.lastMessageId = 0;
            AppConfig.AUTH_TOKEN = null;

            messageContainer.getChildren().clear();
            contactsList.getItems().clear();

            // Load login UI but REUSE the existing scene (keeps CSS)
            FXMLLoader loader = new FXMLLoader(getClass().getResource("/login.fxml"));
            Parent loginRoot = loader.load();

            Stage stage = (Stage) logoutButton.getScene().getWindow();
            Scene scene = stage.getScene();       // reuse
            scene.setRoot(loginRoot);            // swap root
            stage.setTitle("Onyx Chat – Login"); // or just "Onyx Chat"
            stage.show();

        } catch (Exception e) {
            e.printStackTrace();
        }
    }


    // ─────────────────────────────────────────────────────────────
    // WebSocket (push messages, presence, typing)
    // ─────────────────────────────────────────────────────────────

    private void connectWebSocket() {
        if (authToken == null || authToken.isEmpty()) {
            System.err.println("No auth token; cannot open WebSocket.");
            return;
        }

        try {
            HttpClient client = HttpClient.newHttpClient();

            String base = AppConfig.SERVER_BASE_URL;  // e.g. "http://localhost:8080"
            String wsUrl = base.replaceFirst("^http", "ws") + "/api/v1/ws";

            webSocket = client.newWebSocketBuilder()
                    .header("Authorization", "Bearer " + authToken)
                    .buildAsync(URI.create(wsUrl), new WebSocket.Listener() {
                        @Override
                        public void onOpen(WebSocket webSocket) {
                            System.out.println("WebSocket opened");
                            webSocket.request(1);
                        }

                        @Override
                        public CompletionStage<?> onText(
                                WebSocket webSocket,
                                CharSequence data,
                                boolean last
                        ) {
                            try {
                                JSONObject obj = new JSONObject(data.toString());
                                String type = obj.optString("type", "");

                                if ("message".equals(type)) {
                                    JSONObject m = obj.getJSONObject("message");
                                    MessageDto dto = new MessageDto();
                                    dto.id = m.getLong("id");
                                    dto.senderId = m.getLong("senderId");
                                    dto.recipientId = m.getLong("recipientId");
                                    dto.body = m.getString("body");
                                    dto.createdAt = m.getString("createdAt");

                                    String peer = getSelectedContact();
                                    if (peer != null) {
                                        Platform.runLater(() -> addMessageToList(peer, dto));
                                    }

                                } else if ("presence".equals(type)) {
                                    String username = obj.getString("username");
                                    String status = obj.getString("status"); // "online" or "offline"
                                    boolean isOnline = "online".equalsIgnoreCase(status);

                                    boolean isSnapshot = obj.optBoolean("isSnapshot", false);
                                    System.out.println("[WS presence] " + username + " is now " + status +
                                            (isSnapshot ? " (snapshot)" : " (live)"));

                                    onlineStatus.put(username, isOnline);
                                    Platform.runLater(() -> contactsList.refresh());

                                } else if ("typing".equals(type)) {
                                    String from = obj.getString("from");
                                    String to = obj.getString("to");
                                    boolean isTyping = obj.getBoolean("isTyping");

                                    String selected = getSelectedContact();
                                    if (selected == null) {
                                        return null;
                                    }

                                    boolean isForMe = to.equalsIgnoreCase(currentUsername);
                                    boolean isFromSelectedContact = from.equalsIgnoreCase(selected);

                                    if (isForMe && isFromSelectedContact && typingLabel != null) {
                                        Platform.runLater(() -> {
                                            if (isTyping) {
                                                typingLabel.setText(from + " is typing...");
                                                typingLabel.setVisible(true);
                                            } else {
                                                typingLabel.setText("");
                                                typingLabel.setVisible(false);
                                            }
                                        });
                                    }
                                }

                            } catch (Exception e) {
                                e.printStackTrace();
                            }

                            webSocket.request(1);
                            return null;
                        }

                        @Override
                        public void onError(WebSocket webSocket, Throwable error) {
                            System.err.println("WebSocket error: " + error.getMessage());
                        }

                        @Override
                        public CompletionStage<?> onClose(WebSocket webSocket, int statusCode, String reason) {
                            System.out.println("WebSocket closed: " + statusCode + " " + reason);
                            return null;
                        }
                    }).join();

        } catch (Exception e) {
            e.printStackTrace();
        }
    }
}
