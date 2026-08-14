package com.yamiua.app

import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.LinearLayout
import android.widget.TextView
import androidx.core.content.ContextCompat
import androidx.recyclerview.widget.RecyclerView

/** 一条对话气泡。role: "user" 右对齐绿底，"ai" 左对齐中性底。 */
data class ChatMsg(val role: String, val text: String)

/**
 * 对话气泡列表适配器。复用 item_chat.xml（其默认是 AI 左对齐气泡），
 * 在 onBindViewHolder 里按 role 切换对齐方向、背景与文字色，
 * 因此同一个布局文件即可表达 user / ai 两种气泡，无需为 user 另建一份。
 */
class ChatAdapter : RecyclerView.Adapter<ChatAdapter.VH>() {
    private val items = mutableListOf<ChatMsg>()

    fun add(msg: ChatMsg) {
        items.add(msg)
        notifyItemInserted(items.size - 1)
    }

    /** 流式打字机：把最后一条气泡的文本替换为前缀（由调用方逐帧推进）。 */
    fun updateLast(text: String) {
        if (items.isEmpty()) return
        val i = items.size - 1
        items[i] = items[i].copy(text = text)
        notifyItemChanged(i)
    }

    fun clear() {
        items.clear()
        notifyDataSetChanged()
    }

    fun isEmpty() = items.isEmpty()

    class VH(view: View) : RecyclerView.ViewHolder(view) {
        val tvRole: TextView = view.findViewById(R.id.tvRole)
        val tvBubble: TextView = view.findViewById(R.id.tvBubble)
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context).inflate(R.layout.item_chat, parent, false)
        return VH(v)
    }

    override fun onBindViewHolder(holder: VH, position: Int) {
        val m = items[position]
        val ctx = holder.itemView.context
        val root = holder.itemView as LinearLayout
        if (m.role == "user") {
            root.gravity = Gravity.END
            holder.tvRole.text = "你"
            holder.tvBubble.setBackgroundResource(R.drawable.bg_bubble_user)
            holder.tvBubble.setTextColor(ContextCompat.getColor(ctx, R.color.yami_on_accent))
        } else {
            root.gravity = Gravity.START
            holder.tvRole.text = "AI"
            holder.tvBubble.setBackgroundResource(R.drawable.bg_bubble_ai)
            holder.tvBubble.setTextColor(ContextCompat.getColor(ctx, R.color.yami_on_surface))
        }
        holder.tvBubble.text = m.text
    }

    override fun getItemCount() = items.size
}
