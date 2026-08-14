package com.zedscope.app

import android.content.ClipData
import android.content.ClipboardManager
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Toast
import androidx.recyclerview.widget.RecyclerView

/** Card-style adapter for captured requests. Self-contained: binds a List<Record>
 *  and copies the full request detail to the clipboard on tap. */
class CaptureAdapter : RecyclerView.Adapter<CaptureAdapter.VH>() {
    private var items: List<Record> = emptyList()
    fun setItems(list: List<Record>) { items = list; notifyDataSetChanged() }

    class VH(view: View) : RecyclerView.ViewHolder(view) {
        val tvMethod: TextView = view.findViewById(R.id.tvMethod)
        val tvStatus: TextView = view.findViewById(R.id.tvStatus)
        val tvScheme: TextView = view.findViewById(R.id.tvScheme)
        val tvUrl: TextView = view.findViewById(R.id.tvUrl)
        val tvMeta: TextView = view.findViewById(R.id.tvMeta)
        val dotLogin: View = view.findViewById(R.id.dotLogin)
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context).inflate(R.layout.item_capture, parent, false)
        return VH(v)
    }

    override fun onBindViewHolder(holder: VH, position: Int) {
        val r = items[position]
        holder.tvMethod.text = r.method
        holder.tvStatus.text = r.status_code.toString()
        holder.tvScheme.text = if (r.is_https) "HTTPS" else "HTTP"
        holder.tvUrl.text = r.url
        holder.tvMeta.text = "${r.host} · token ${r.tokens.size}"
        holder.dotLogin.visibility = if (r.is_login) View.VISIBLE else View.GONE
        holder.itemView.setOnClickListener {
            val detail = buildString {
                append("${r.method} ${r.url}\n")
                append("status: ${r.status_code}\n")
                append("tokens: ${r.tokens.size}\n")
                r.tokens.forEach { append("  ${it.key} = ${it.value}\n") }
            }
            val cm = holder.itemView.context.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText("ZedScope request", detail))
            Toast.makeText(holder.itemView.context, "已复制请求详情", Toast.LENGTH_SHORT).show()
        }
    }

    override fun getItemCount() = items.size
}
