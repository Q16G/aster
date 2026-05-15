class IdorOwnershipDrop {
    Order queryOrder(String currentUserId, String orderId, OrderMapper mapper) {
        // ruleid: java-misc-idor-ownership-drop
        return mapper.selectOrder(orderId);
    }

    void deleteTicket(String operatorId, String targetId, TicketMapper mapper) {
        // ruleid: java-misc-idor-ownership-drop
        mapper.deleteTicket(targetId);
    }

    Order safeQuery(String currentUserId, String orderId, OrderMapper mapper) {
        // ok: java-misc-idor-ownership-drop
        return mapper.selectOrder(currentUserId, orderId);
    }

    void safeDelete(String operatorId, String targetId, TicketMapper mapper) {
        // ok: java-misc-idor-ownership-drop
        mapper.deleteTicket(operatorId, targetId);
    }
}

class Order {}

class OrderMapper {
    Order selectOrder(String orderId) { return null; }
    Order selectOrder(String currentUserId, String orderId) { return null; }
}

class TicketMapper {
    void deleteTicket(String targetId) {}
    void deleteTicket(String operatorId, String targetId) {}
}
